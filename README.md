# mongodbatlas-krm-extensions

Kubernetes kinds for the MongoDB Atlas configuration that Terraform's
`mongodb/mongodbatlas` provider manages and MongoDB Atlas's operators do not. MongoDB's official Atlas Kubernetes Operator manages projects, deployments, database users, network access, backups, private endpoints, stream processing and more (24 CRDs).

By the numbers: `mongodb/mongodbatlas` is the #37 Terraform provider by downloads
(102,966,030); version 2.21.0 has 85 resources. Operators
cover 32. This repository adds **53 kinds** standing in for
**53 more** of the other 53. Each gap kind is listed below with its
Terraform equivalent; the 1 it does not cover, and why, is under
"Not covered".

**Limits.** Each operation sends the Atlas API version its spec names (Accept: application/vnd.atlas.<date>+json). Authentication is a service account (clientId, clientSecret); API-key digest auth is not supported. `EventTrigger` is the App Services Admin API's: its Connection is one of its own (baseURL `https://services.cloud.mongodb.com`), whose Secret holds a programmatic API key, `publicKey` and `privateKey`, which the manager signs in with (`POST /api/admin/v3.0/auth/providers/mongodb-cloud/login`), as Terraform does.

**Readiness: 17 of 53 kinds live.** Every kind passes create, update, drift
repair and delete against a fake built from the same descriptors
(`TestEveryKindOnFake`). Seventeen have also passed against the real Atlas API,
in a free organization (`hack/live/records.json`; readiness `live` in
`zz_generated.report.md`). The rest need what a free organization lacks
(a dedicated cluster, Cloud Backup, a cloud IAM role or KMS, billed Stream
Processing) or would change the organization itself; `hack/live/live.yaml`
says which, kind by kind. Treat those as untested against the real API.

What the live run found about Atlas:

- An update refuses some fields the create takes (HTTP 400
  INVALID_ATTRIBUTE): a service account's `secretExpiresAfterHours` and
  `withoutInitialSecret`, an invitation's `username`, an AI model key's
  `cloud` and `geography`. `hooks.go` drops them once the object exists; a
  change to one is not applied.
- A read of a deleted API key answers HTTP 400 `API_KEY_NOT_FOUND`, not 404;
  `vendor.go` treats it as not found.
- An API key's `roles` are written as names and read back as
  `[{orgId, roleName}]`, so they are not compared (`overrides.writeOnly`);
  a change in the spec is still sent.
- Deleting a `ProjectServiceAccount` only removes it from the project, as the
  API documents: the organization-level service account and its secret stay.
- Atlas wants a versioned media type on every call; it answers a date with
  the latest version on or before it, so calls with none of their own send
  2025-03-12.

Kinds the live run has not reached yet, and what to know about them:

- `ServiceAccountAccessListEntry`, `ProjectServiceAccountAccessListEntry`
  (and `AccessListApiKey`) take their ID from `ipAddress`: set it. An entry
  given only a `cidrBlock` is made, but the object gets no ID to read or
  delete it by. The two service account kinds find the entry in the access
  list (up to 500 entries), as Terraform does: the API cannot get one.
- `CloudUserTeamAssignment` adds and removes a team member with
  `POST .../teams/{teamId}/users` and `DELETE .../users/{userId}`, which
  Atlas has deprecated for `:addUser` and `:removeUser` (sunset
  2027-07-01), and finds the member in the team's user list.
- `CloudUserOrgAssignment` and `CloudUserProjectAssignment` delete with
  API version 2023-01-01, which, as the spec says, removes only active
  users: a user who has not accepted the invitation yet is not removed
  (2025-02-19 removes both). The live run skips them for that, since a
  pending `@example.com` user is all it can make.
- `CloudUserProjectAssignment` cannot be updated: Atlas changes a project
  member's roles with `:addRole` and `:removeRole` calls, so a role change
  is reported as drift; re-create the object to apply it.
- `LdapConfiguration` is the project's `userSecurity` document: deleting
  the object leaves LDAP as it is (Terraform's delete turns authentication
  off). `ldap.bindPassword` is never read back, so it is not compared;
  it sits in the spec, since `spec.secretRefs` fills top-level fields only.
- `CloudBackupSnapshotExportJob`, `CloudBackupCollectionRestoreJob` and
  `LdapVerify` are one-off jobs Atlas cannot delete or cancel: deleting the
  object lets go of the job and leaves it in Atlas, as Terraform does. They
  cannot be updated either; a changed spec is reported as drift, so
  re-create the object to run the job again. `LdapVerify` reads its request
  back under `request` (and never `bindPassword`), so its fields are not
  compared; put `bindPassword` in a Secret (`spec.secretRefs`).
- `ClusterOutageSimulation` starts a simulation with
  `POST .../outageSimulation` and ends it with `DELETE` on the same path.
  It cannot be updated (Terraform refuses an update too): a changed
  `outageFilters` is sent as a new start, which Atlas refuses while one
  runs; delete the object and create it again. While the object exists the
  outage is wanted: if the simulation is ended outside Kubernetes, or
  Atlas ends it at its `expirationDate` (three days by default), the next
  read finds none and starts it again. Terraform waits for a simulation to
  be running before it ends one; the kind's delete is retried until Atlas
  accepts it.

## Not covered

<!-- BEGIN generated: not covered (krmgen.py, from vendor.yaml hardBlocks and draftNotes) -->
Every one of the 53 Terraform gap types has a kind.
<!-- END generated: not covered -->

`ProjectApiKey` (`mongodbatlas_project_api_key`) is an organization API
key made in a project (`POST .../groups/{groupId}/apiKeys`): its
`spec.orgId` names the organization it is deleted from, with the object,
and its private key goes to `spec.writeSecretRef`. Terraform's further
`project_assignment` blocks are each an `ApiKeyProjectAssignment`, which
gives an existing key (an `ApiKey`, or this one) roles in another project;
its description is changed on the organization's key (`ApiKey`). The list
a project's keys are read from answers roles as `[{groupId, roleName}]`, so
`roles` are not compared: a change in the spec is still sent.

`ServiceAccountProjectAssignment` gives an organization's service account
(`ServiceAccount`) roles in a project (`...:invite`); deleting it removes
the account from the project only, unlike a `ProjectServiceAccount`
(`hooks.go`). `MongodbEmployeeAccessGrant` is a cluster's MongoDB support
access, read from the cluster; deleting the object revokes it.

`GlobalClusterConfig` (`mongodbatlas_global_cluster_config`) holds a
global cluster's managed namespaces and custom zone mappings, both read
from `GET .../globalWrites`. `managedNamespaces` lists every namespace,
each named by its `db` and `collection`: a missing one is added, one not
listed is removed (`DELETE .../managedNamespaces?db=...&collection=...`),
and a listed one that differs is refused, since Atlas cannot change one in
place (Terraform refuses that too): remove it, then add it back.
`customZoneMappings`, if set, are added in one call with only the new
ones; Atlas removes mappings only all together, so a spec that drops or
changes one is refused unless it lists none, which removes them all (as
Terraform does). Atlas answers each mapping's zone as the zone's ID, kept
in `status.atProvider.customZoneMapping` (Terraform's
`custom_zone_mapping_zone_id`); a zone changed outside Kubernetes is not
noticed, as Terraform does not notice it either. Deleting the object
removes the namespaces and, if it lists any, all mappings. The live run
skips it: it needs a global (geo-sharded) dedicated cluster.

## How it works

The kinds are generated, not hand-written. `hack/gen/vendor.yaml` pins
MongoDB Atlas's OpenAPI spec; [krm-rest-runtime](https://github.com/highfivelabs/krm-rest-runtime)'s
generator finds every resource the API exposes in a create/read/update/delete
shape, keeps those that stand in for a Terraform resource in the gap list
(`hack/gen/terraform-gaps.json`), and writes:

- `types.yaml`: per kind, its API calls, path parameters and Terraform equivalents
- `config/crds/`: one CRD per kind, its spec the API's request body
- `zz_generated.report.md`: the full list

One generic manager (`cmd/manager`) runs every kind on krm-rest-runtime's
reconciler: it creates the resource, reads it back every 10 minutes and
puts back anything changed outside Kubernetes, and deletes it with the
object. Spec fields keep MongoDB Atlas's own names, so the API reference
applies field for field. Path parameters are spec fields too.

Hand corrections to the automatic matching (a Terraform type matched to
the wrong API path, or not matched) are in `vendor.yaml`'s `overrides`.

## Install

```sh
kubectl kustomize config/default | KO_DOCKER_REPO=<registry> ko resolve -f - | kubectl apply --server-side -f -
```

Then, in each namespace that manages MongoDB Atlas, a Secret with
`clientId`, `clientSecret` and a Connection named `default`
(`config/samples/connection.yaml`). Objects use it unless
`spec.connectionRef.name` names another. Secret values (tokens, passwords,
secret values) go in a Kubernetes Secret, not the spec:
`spec.secretRefs: {<field>: {name, key}}` fills that body field.

To run only some API groups, install those groups' CRDs from `config/crds`
and pass `--groups` to the manager.

## Kinds

| Kind | Terraform equivalent | Shape | Read |
| --- | --- | --- | --- |
| `access.mongodbatlas.hfl.dev/v1alpha1` `AccessListApiKey` | `mongodbatlas_access_list_api_key` | crud | `GET /api/atlas/v2/orgs/{orgId}/apiKeys/{apiUserId}/accessList/{ipAddress}` |
| `ai.mongodbatlas.hfl.dev/v1alpha1` `AiModelApiKey` | `mongodbatlas_ai_model_api_key` | crud | `GET /api/atlas/v2/groups/{groupId}/aiModelApiKeys/{apiKeyId}` |
| `ai.mongodbatlas.hfl.dev/v1alpha1` `AiModelRateLimit` | `mongodbatlas_ai_model_rate_limit` | settings | `GET /api/atlas/v2/groups/{groupId}/aiModelApiClouds/{cloud}/geographies/{geography}/modelGroupNames/{modelGroupName}/rateLimits` |
| `api.mongodbatlas.hfl.dev/v1alpha1` `ApiKey` | `mongodbatlas_api_key` | crud | `GET /api/atlas/v2/orgs/{orgId}/apiKeys/{apiUserId}` |
| `api.mongodbatlas.hfl.dev/v1alpha1` `ApiKeyProjectAssignment` | `mongodbatlas_api_key_project_assignment` | upsert | `GET /api/atlas/v2/groups/{groupId}/apiKeys` (finds `id`) |
| `cloud.mongodbatlas.hfl.dev/v1alpha1` `CloudBackupCollectionRestoreJob` | `mongodbatlas_cloud_backup_collection_restore_job` | crud | `GET /api/atlas/v2/groups/{groupId}/clusters/{clusterName}/collectionRestoreJobs/{jobId}` |
| `cloud.mongodbatlas.hfl.dev/v1alpha1` `CloudBackupSnapshot` | `mongodbatlas_cloud_backup_snapshot` | crud | `GET /api/atlas/v2/groups/{groupId}/clusters/{clusterName}/backup/snapshots/{snapshotId}` |
| `cloud.mongodbatlas.hfl.dev/v1alpha1` `CloudBackupSnapshotExportBucket` | `mongodbatlas_cloud_backup_snapshot_export_bucket` | crud | `GET /api/atlas/v2/groups/{groupId}/backup/exportBuckets/{exportBucketId}` |
| `cloud.mongodbatlas.hfl.dev/v1alpha1` `CloudBackupSnapshotExportJob` | `mongodbatlas_cloud_backup_snapshot_export_job` | crud | `GET /api/atlas/v2/groups/{groupId}/clusters/{clusterName}/backup/exports/{exportId}` |
| `cloud.mongodbatlas.hfl.dev/v1alpha1` `CloudBackupSnapshotRestoreJob` | `mongodbatlas_cloud_backup_snapshot_restore_job` | crud | `GET /api/atlas/v2/groups/{groupId}/clusters/{clusterName}/backup/restoreJobs/{restoreJobId}` |
| `cloud.mongodbatlas.hfl.dev/v1alpha1` `CloudUserOrgAssignment` | `mongodbatlas_cloud_user_org_assignment` | crud | `GET /api/atlas/v2/orgs/{orgId}/users/{userId}` |
| `cloud.mongodbatlas.hfl.dev/v1alpha1` `CloudUserProjectAssignment` | `mongodbatlas_cloud_user_project_assignment` | crud | `GET /api/atlas/v2/groups/{groupId}/users/{userId}` |
| `cloud.mongodbatlas.hfl.dev/v1alpha1` `CloudUserTeamAssignment` | `mongodbatlas_cloud_user_team_assignment` | crud | `GET /api/atlas/v2/orgs/{orgId}/teams/{teamId}/users` |
| `cluster.mongodbatlas.hfl.dev/v1alpha1` `ClusterAdaptiveSettings` | `mongodbatlas_cluster_adaptive_settings` | settings | `GET /api/atlas/v2/groups/{groupId}/clusters/{clusterName}/adaptiveSettings` |
| `cluster.mongodbatlas.hfl.dev/v1alpha1` `ClusterOutageSimulation` | `mongodbatlas_cluster_outage_simulation` | upsert | `GET /api/atlas/v2/groups/{groupId}/clusters/{clusterName}/outageSimulation` |
| `cluster.mongodbatlas.hfl.dev/v1alpha1` `ClusterOverloadSimulation` | `mongodbatlas_cluster_overload_simulation` | crud | `GET /api/atlas/v2/groups/{groupId}/clusters/{clusterName}/overloadSimulations/{simulationId}` |
| `encryption.mongodbatlas.hfl.dev/v1alpha1` `EncryptionAtRestPrivateEndpoint` | `mongodbatlas_encryption_at_rest_private_endpoint` | crud | `GET /api/atlas/v2/groups/{groupId}/encryptionAtRest/{cloudProvider}/privateEndpoints/{endpointId}` |
| `event.mongodbatlas.hfl.dev/v1alpha1` `EventTrigger` | `mongodbatlas_event_trigger` | crud | `GET /api/admin/v3.0/groups/{groupId}/apps/{appId}/triggers/{id}` |
| `federated.mongodbatlas.hfl.dev/v1alpha1` `FederatedQueryLimit` | `mongodbatlas_federated_query_limit` | upsert | `GET /api/atlas/v2/groups/{groupId}/dataFederation/{tenantName}/limits/{limitName}` |
| `ldap.mongodbatlas.hfl.dev/v1alpha1` `LdapConfiguration` | `mongodbatlas_ldap_configuration` | settings | `GET /api/atlas/v2/groups/{groupId}/userSecurity` |
| `ldap.mongodbatlas.hfl.dev/v1alpha1` `LdapVerify` | `mongodbatlas_ldap_verify` | crud | `GET /api/atlas/v2/groups/{groupId}/userSecurity/ldap/verify/{requestId}` |
| `log.mongodbatlas.hfl.dev/v1alpha1` `LogIntegration` | `mongodbatlas_log_integration` | crud | `GET /api/atlas/v2/groups/{groupId}/logIntegrations/{id}` |
| `mcp.mongodbatlas.hfl.dev/v1alpha1` `McpConfig` | `mongodbatlas_mcp_config` | crud | `GET /api/atlas/v2/orgs/{orgId}/mcpConfigs/{mcpConfigId}` |
| `mcp.mongodbatlas.hfl.dev/v1alpha1` `McpConfigSecret` | `mongodbatlas_mcp_config_secret` | crud | `GET /api/atlas/v2/orgs/{orgId}/mcpConfigs/{mcpConfigId}/secrets/{secretId}` |
| `metric.mongodbatlas.hfl.dev/v1alpha1` `MetricIntegration` | `mongodbatlas_metric_integration` | crud | `GET /api/atlas/v2/groups/{groupId}/metricIntegrations/{metricIntegrationId}` |
| `mongodb.mongodbatlas.hfl.dev/v1alpha1` `MongodbEmployeeAccessGrant` | `mongodbatlas_mongodb_employee_access_grant` | upsert | `GET /api/atlas/v2/groups/{groupId}/clusters/{clusterName}` |
| `online.mongodbatlas.hfl.dev/v1alpha1` `OnlineArchive` | `mongodbatlas_online_archive` | crud | `GET /api/atlas/v2/groups/{groupId}/clusters/{clusterName}/onlineArchives/{archiveId}` |
| `org.mongodbatlas.hfl.dev/v1alpha1` `OrgDelegationSettings` | `mongodbatlas_org_delegation_settings` | settings | `GET /api/atlas/v2/orgs/{orgId}/delegationSettings` |
| `org.mongodbatlas.hfl.dev/v1alpha1` `OrgInvitation` | `mongodbatlas_org_invitation` | crud | `GET /api/atlas/v2/orgs/{orgId}/invites/{invitationId}` |
| `org.mongodbatlas.hfl.dev/v1alpha1` `OrgLogIntegration` | `mongodbatlas_org_log_integration` | crud | `GET /api/atlas/v2/orgs/{orgId}/logIntegrations/{logIntegrationId}` |
| `org.mongodbatlas.hfl.dev/v1alpha1` `OrgMaintenanceSettings` | `mongodbatlas_org_maintenance_settings` | settings | `GET /api/atlas/v2/orgs/{orgId}/maintenanceSettings` |
| `organization.mongodbatlas.hfl.dev/v1alpha1` `Organization` | `mongodbatlas_organization` | crud | `GET /api/atlas/v2/orgs/{orgId}` |
| `private.mongodbatlas.hfl.dev/v1alpha1` `PrivateEndpointRegionalMode` | `mongodbatlas_private_endpoint_regional_mode` | settings | `GET /api/atlas/v2/groups/{groupId}/privateEndpoint/regionalMode` |
| `privatelink.mongodbatlas.hfl.dev/v1alpha1` `PrivatelinkEndpointServiceDataFederationOnlineArchive` | `mongodbatlas_privatelink_endpoint_service_data_federation_online_archive` | crud | `GET /api/atlas/v2/groups/{groupId}/privateNetworkSettings/endpointIds/{endpointId}` |
| `project.mongodbatlas.hfl.dev/v1alpha1` `ProjectApiKey` | `mongodbatlas_project_api_key` | crud | `GET /api/atlas/v2/groups/{groupId}/apiKeys` (finds `id`) |
| `project.mongodbatlas.hfl.dev/v1alpha1` `ProjectInvitation` | `mongodbatlas_project_invitation` | crud | `GET /api/atlas/v2/groups/{groupId}/invites/{invitationId}` |
| `project.mongodbatlas.hfl.dev/v1alpha1` `ProjectMcpConfig` | `mongodbatlas_project_mcp_config` | crud | `GET /api/atlas/v2/groups/{groupId}/mcpConfigs/{mcpConfigId}` |
| `project.mongodbatlas.hfl.dev/v1alpha1` `ProjectMcpConfigSecret` | `mongodbatlas_project_mcp_config_secret` | crud | `GET /api/atlas/v2/groups/{groupId}/mcpConfigs/{mcpConfigId}/secrets/{secretId}` |
| `project.mongodbatlas.hfl.dev/v1alpha1` `ProjectServiceAccount` | `mongodbatlas_project_service_account` | crud | `GET /api/atlas/v2/groups/{groupId}/serviceAccounts/{clientId}` |
| `project.mongodbatlas.hfl.dev/v1alpha1` `ProjectServiceAccountAccessListEntry` | `mongodbatlas_project_service_account_access_list_entry` | crud | `GET /api/atlas/v2/groups/{groupId}/serviceAccounts/{clientId}/accessList` |
| `project.mongodbatlas.hfl.dev/v1alpha1` `ProjectServiceAccountSecret` | `mongodbatlas_project_service_account_secret` | crud | `GET /api/atlas/v2/groups/{groupId}/serviceAccounts/{clientId}` |
| `push.mongodbatlas.hfl.dev/v1alpha1` `PushBasedLogExport` | `mongodbatlas_push_based_log_export` | upsert | `GET /api/atlas/v2/groups/{groupId}/pushBasedLogExport` |
| `resource.mongodbatlas.hfl.dev/v1alpha1` `ResourcePolicy` | `mongodbatlas_resource_policy` | crud | `GET /api/atlas/v2/orgs/{orgId}/resourcePolicies/{resourcePolicyId}` |
| `search.mongodbatlas.hfl.dev/v1alpha1` `SearchDeployment` | `mongodbatlas_search_deployment` | upsert | `GET /api/atlas/v2/groups/{groupId}/clusters/{clusterName}/search/deployment` |
| `service.mongodbatlas.hfl.dev/v1alpha1` `ServiceAccount` | `mongodbatlas_service_account` | crud | `GET /api/atlas/v2/orgs/{orgId}/serviceAccounts/{clientId}` |
| `service.mongodbatlas.hfl.dev/v1alpha1` `ServiceAccountAccessListEntry` | `mongodbatlas_service_account_access_list_entry` | crud | `GET /api/atlas/v2/orgs/{orgId}/serviceAccounts/{clientId}/accessList` |
| `service.mongodbatlas.hfl.dev/v1alpha1` `ServiceAccountProjectAssignment` | `mongodbatlas_service_account_project_assignment` | upsert | `GET /api/atlas/v2/groups/{groupId}/serviceAccounts/{clientId}` |
| `service.mongodbatlas.hfl.dev/v1alpha1` `ServiceAccountSecret` | `mongodbatlas_service_account_secret` | crud | `GET /api/atlas/v2/orgs/{orgId}/serviceAccounts/{clientId}` |
| `stream.mongodbatlas.hfl.dev/v1alpha1` `StreamConnectionFailover` | `mongodbatlas_stream_connection_failover` | crud | `GET /api/atlas/v2/groups/{groupId}/streams/{tenantName}/connections/{connectionName}/failoverConnections/{failoverConnectionId}` |
| `stream.mongodbatlas.hfl.dev/v1alpha1` `StreamPrivatelinkEndpoint` | `mongodbatlas_stream_privatelink_endpoint` | crud | `GET /api/atlas/v2/groups/{groupId}/streams/privateLinkConnections/{connectionId}` |
| `stream.mongodbatlas.hfl.dev/v1alpha1` `StreamProcessor` | `mongodbatlas_stream_processor` | crud | `GET /api/atlas/v2/groups/{groupId}/streams/{tenantName}/processor/{processorName}` |
| `stream.mongodbatlas.hfl.dev/v1alpha1` `StreamWorkspace` | `mongodbatlas_stream_workspace` | crud | `GET /api/atlas/v2/groups/{groupId}/streams/{tenantName}` |

## For agents (MCP)

`cmd/mcp` serves this repository's kinds over the Model Context Protocol
(stdio), from `types.yaml`, `config/crds` and the live tests' records.
`.mcp.json` registers it, so an agent session opened in this repository
has it without setup. Elsewhere, run it from the repository root:

```sh
go run ./cmd/mcp
```

Its tools: `list_kinds`, `find_kinds_for_terraform`, `describe_kind` (every
field, the REST calls, the live record), `example` (the live-tested spec
where there is one), `validate_manifest` (the CRD schema, as the API server
checks it) and `plan_on_fake` (the controller run against the fake API,
with every REST call it makes). Nothing it does reaches the real API.
krm-rest-runtime's README ("For agents") has the details.

## Regenerate

```sh
hack/gen/generate.sh   # needs python3 + pyyaml, and krm-rest-runtime beside this repo
go test ./...
```
