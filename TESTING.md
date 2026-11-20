# Testing the Laravel Cloud Terraform Provider

## Unit Tests

```sh
make test
```

## Acceptance Tests

Acceptance tests create **real resources** in Laravel Cloud. They require a valid API token and will incur usage.

### Prerequisites

Set the following environment variables:

```sh
export LARAVEL_CLOUD_API_TOKEN="your-api-token"
```

Optionally override the API base URL (defaults to `https://cloud.laravel.com/api`):

```sh
export LARAVEL_CLOUD_BASE_URL="https://your-api-host/api"
```

### Running All Acceptance Tests

```sh
make testacc
```

Acceptance tests build from a real source repository, which defaults to
`laravel/laravel` on branch `13.x`. Override either if that repository is not
connected to the account under test:

```sh
export LARAVEL_CLOUD_TEST_REPOSITORY="your-org/your-repo"
export LARAVEL_CLOUD_TEST_BRANCH="main"
```

The database cluster acceptance test is **commented out**. Provisioning a
cluster routinely takes twenty minutes or more, and neither the databases
inside it nor the cluster itself can be deleted until it finishes, so a
create-then-destroy cycle either blocks the run for a long time or leaves a
billable database behind. Its behaviour is covered by the plan tests below,
which exercise the same code paths against the in-memory fake in under a
second. Uncomment it in `internal/provider/resource_database_cluster_test.go`
to run it against a real API.

### Running a Specific Test

```sh
TF_ACC=1 go test ./internal/provider/ -v -run TestAccApplicationResource_basic -timeout 60m
```

### Debugging

Enable Terraform debug logging:

```sh
TF_LOG=DEBUG make testacc
```

### Cleaning Up (Sweepers)

Every acceptance test names its resources with the `tfacc-` prefix (use
`testAccName()`). The sweepers delete only resources with that prefix, so they
are safe to run against an organization that also holds real resources:

```sh
go test ./internal/provider -v -sweep=all
```

They delete applications (and with them their environments), WebSocket
servers, caches, buckets (keys first), database clusters (databases first) and
secrets.

By default a sweep keeps anything created in the last two hours, because other
test runs may be using the same organization at the same time. Set
`LARAVEL_CLOUD_SWEEP_MIN_AGE` to change that, as a Go duration such as `30m`,
or `0` to delete every `tfacc-` resource once you know nothing else is testing.

## Live Testing with Real Terraform

Acceptance tests drive Terraform from Go. To run real `terraform plan` and
`apply` by hand against a dev build, and see every API call it makes, use the
scenarios in
[laravel-gtm/terraform-provider-sandbox](https://github.com/laravel-gtm/terraform-provider-sandbox).
Its README has the full workflow; in short:

1. Build the provider: `go build -o bin/terraform-provider-laravel .`
2. Point Terraform at the build with `dev_overrides` in `~/.terraformrc`
   (template in the sandbox repo). Terraform prints "Provider development
   overrides are in effect" when it is used, and no `terraform init` is needed.
3. Run a scenario: `bin/run-scenario scenarios/<name>`. It applies each step,
   checks that a second plan is empty, destroys everything, and saves the
   Terraform output and API calls to `runs/<scenario>/<timestamp>/`.

Scenarios that deploy use
[laravel-gtm/terraform-sandbox-app](https://github.com/laravel-gtm/terraform-sandbox-app),
a plain Laravel skeleton.

### API Request Log

Set `LARAVEL_CLOUD_HTTP_LOG` to a file path and the provider appends one JSON
line per API call: time, method, URL, status, duration, and the request and
response bodies. The token is never written, and passwords, secrets, tokens,
variable and secret values, and deploy hook URLs are redacted.

```sh
export LARAVEL_CLOUD_HTTP_LOG=/tmp/api.jsonl
terraform apply
jq -r 'select(.method != "GET") | "\(.method) \(.url) -> \(.status)"' /tmp/api.jsonl
```

### Fixing a Bug

1. Reproduce it with real Terraform, ideally as a sandbox scenario, and keep
   the plan output and API log from a released build.
2. Write an acceptance test that fails on the current code.
3. Fix it, then update the fake API in `mock_*_test.go` to behave like the
   live API did, and add a plan test so the bug is caught on every PR.
4. Run the scenario again with the fix and keep the after evidence.

### CI

The Acceptance Tests job runs on pushes to `main`, same-repository pull
requests and manually started runs. It needs the `LARAVEL_CLOUD_API_TOKEN`
repository secret; without it every acceptance test skips and the job still
passes, so check the job log for `--- PASS: TestAcc` rather than the job status.

## Plan-Check Tests (fake API)

Plan-check tests run a **real** `terraform plan`/`apply` cycle against an
in-memory fake of the Laravel Cloud API (`internal/provider/mock_cloud_test.go`).
They assert that the plan and applied state match what the configuration asks
for — for example, that the `examples/basic-app` config plans cleanly, that
`php_version = "8.4:1"` is read back as `php_major_version = "8.4"`, and that
re-applying the same config is a no-op (no perpetual diff).

Unlike acceptance tests, they **create no real resources** and need **no API
token or network** — only `TF_ACC=1` (required by the testing framework to run
any plan/apply) and a `terraform` binary on `PATH`.

```sh
make testplan
```

Or directly:

```sh
TF_ACC=1 go test ./internal/provider/ -v -run 'Plan$' -timeout 5m
```

Without `TF_ACC=1` these tests are skipped, so they do not run as part of
`make test`.

### Coverage

Every resource has a plan-check test that runs a full create → apply →
re-plan (empty-plan) cycle against the fake API.

> Note: `connection_details` on the cache, database cluster, and database
> restore resources is modeled as a `types.Object`. A Go pointer-struct
> (`*XConnectionModel`) cannot hold an `unknown` value, so the previous modeling
> made `Create`'s `req.Plan.Get` fail with *"Received unknown value, however the
> target type cannot handle unknown values"* — these resources could be planned
> but not applied. The `types.Object` modeling fixes that; the tests above guard
> against a regression.

## Writing Tests

Tests follow the standard Terraform provider testing patterns using `terraform-plugin-testing`:

1. Use `testAccPreCheck(t)` in every test's `PreCheck` to validate the API token
2. Use `testAccProtoV6ProviderFactories` as the provider factory
3. Name resources with `testAccName()`, which adds the `tfacc-` prefix the sweepers rely on
4. Include both create and import test steps
5. Test updates in a separate test function with multiple steps
