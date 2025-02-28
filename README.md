# onyxia-janitor
Onyxia-Janitor is an application written in Go for managing services deployed into Onyxia/Dapla Lab

## How it works:

Onyxia Janitor is designed to be run as either a one-off job or on a schedule using e.g. Kubernetes CronJobs.
The application is configured through environment variables, where you choose which "action" to perform,
which Helm repositories (and charts) to use, and which filters to apply.
The basic procedure of the application is as follows:

1. Read and validate configuration
2. List all Onyxia services in the cluster (secrets of type `onyxia.sh/release.v1`)
3. Filter list based on the Onyxia metadata
4. Filter list so only Onyxia services from one of the configured catalogs are included
5. Map this list of Onyxia services to an enriched type which includes the Helm release
6. Filter this list based on the Helm release data
7. Perform an action on the resulting list

## Configuration

Onyxia Janitor is configured entirely through environment variables.
Some are required regardless of which action you want to perform,
and some are needed exclusively by the `notify` action.

### Common configuration
| Name | Description | Required | Default |
| ---- | ----------- | -------- | ------- |
| ONYXIA_JANITOR_ACTION | One of [suspend, notify, uninstall] | x | |
| ONYXIA_JANITOR_ONYXIA_CATALOGS | [YAML map of catalogs](#onyxia-catalog-configuration) | x | |
| ONYXIA_JANITOR_ONYXIA_METADATA_FILTER | [expr-lang](https://expr-lang.org/docs/language-definition) filter acting on the [Service](pkg/onyxia/client.go#L28) struct | | `true` |
| ONYXIA_JANITOR_HELM_RELEASE_FILTER | [expr-lang](https://expr-lang.org/docs/language-definition) filter acting on the [Release](https://pkg.go.dev/helm.sh/helm/v3@v3.17.1/pkg/release#Release) struct | | `true` |

### Extra configuration for `notify` action

The `notify` action communicates with Dapla Team API and so needs a few extra environment variables.

| Name | Description | Required | Default |
| ---- | ----------- | -------- | ------- |
| ONYXIA_JANITOR_CLIENT_SECRET | Client secret for Onyxia Janitor's Keycloak client | x | |
| ONYXIA_JANITOR_CLIENT_ID | Client ID for Onyxia Janitor's Keycloak client | x | |
| ONYXIA_JANITOR_TOKEN_URL | OIDC endpoint for fetching access token | x |
| ONYXIA_JANITOR_TEAM_API_URL | Base URL for for the Dapla Team API instance to use | x | |

In addition, we need templates for creating emails, these are written using Go's built-in
[html/template](https://pkg.go.dev/html/template) package, which should feel familiar from e.g.
Helm charts. Included are also all of the functions in the [sprig](https://masterminds.github.io/sprig/) library.
The context for the templates is the [ServicesAndUserInfo](pkg/action/notify/notify.go#L14) struct.

| Name | Description | Required | Default |
| ---- | ----------- | -------- | ------- |
| ONYXIA_JANITOR_SUBJECT_TEMPLATE | Template used to render the email's subject line | x | |
| ONYXIA_JANITOR_BODY_TEMPLATE | Tempalte used to render the email's body | x | |

Here's an example (more or less ready for use) for subject and body templates:

```
SUBJECT
Dapla Lab Dev: Du har tjenester som ble starter for mer enn 7 dager siden

BODY
<html>
  <body>
    <p>Hei {{ .UserInfo.DisplayName }},</p>
    <p>[Du har en eller flere tjenester](https://lab.url/my-services) (se tabell under) som ble startet for mer enn 7 dager siden.</p>
    <p>Vi anbefaler deg å slette tjenester som ble startet for mer enn 7 dager siden slik at du jobber på siste versjon av tjenesten.</p>

    <table border='1' style='border-collapse: collapse;'>
      <tr>
         <th>Tjeneste</th>
        <th>Antall dager</th>
      </tr>
      {{ range .Services }}
      <tr>
         <td>{{ .Service.FriendlyName }}</td>
         <td>{{ div (.Release.Info.FirstDeployed.Time | now.Sub).Hours 24 }}</td>
      </tr>
      {{ end }}
    </table>

     <p>Husk å pushe kode til GitHub, og ta vare på andre filer som ligger i tjenestens filsystem, før du sletter.</p>
  </body>
</html>
```


### Onyxia catalog configuration

The `ONYXIA_JANITOR_ONYXIA_CATALOGS` variable should be a map from the catalog name - as
configured in your Onyxia instance - to an object with properties `[url, filter]`.
`url` is required, while `filter` is optional. If given, it should be an [expr-lang](https://expr-lang.org/docs/language-definition) acting
on a [Chart](http://pkg.go.dev/helm.sh/helm/v3@v3.17.1/pkg/chart#Chart) struct. The filter
is validated on startup.


## Project structure:

### `cmd/`

Contains the main executable code and definitions of environment variables.

### `pkg/action`

Contains subpackages for the different actions. An action should implement the `ServiceWithReleaseAction`
interface from `cmd/main.go`.

### `pkg/onyxia`

Handles listing and parsing of Onyxia secrets in the cluster.

### `pkg/pipe`

Helper package for creating concurrent pipelines.

### `pkg/teamapi`

Handles communication with Dapla Team API.
Used to get user info and send emails.

### `pkg/template`

Wrapper package for `html/template`, and adds the `sprig` template function library.
Used for templating emails.

## Adding more jobs etc.

The aim of the app is that we can easily add more functionality if needed.

To add your code define a new package for the new action under `pkg/action`,
make sure it implements the `ServiceWithReleaseAction` interface.
Add a new case to the action switch block in `cmd/main.go`.
