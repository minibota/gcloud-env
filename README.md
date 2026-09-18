# gcloud-env

A tiny Go TUI for switching between existing Google Cloud CLI configurations **and their matching Application Default Credentials (ADC)**.

`gcloud` named configurations solve only part of the problem: they switch the active CLI account/project, while tools such as Cloud SQL Auth Proxy may authenticate with ADC from `~/.config/gcloud/application_default_credentials.json`. `gcloud-env` keeps one saved ADC file per gcloud configuration and restores the right one when you switch.

## What it does

```text
 gcloud-env
 Changes gcloud configuration and restores its saved ADC.

 › ●  default                 ADC ✓
      pavel@example.com       western-lambda-159601

      minibota                ADC ✓
      pavel@minibota.com      minibota

 ↑/↓ or j/k move   Enter switch   a authenticate ADC   r reload   q quit
```

- Detects existing configurations using `gcloud config configurations list`.
- Shows each configuration's account and project.
- Marks the currently active configuration.
- Switches the active gcloud configuration with `Enter`.
- Restores the ADC previously saved for that configuration.
- Runs `gcloud auth application-default login` with `a` and saves the resulting ADC for future switches.
- Also supports non-interactive `use` and `status` commands.
- Has no Go runtime dependencies: the built executable is a single binary.

## Requirements

- Linux or macOS terminal.
- Google Cloud CLI (`gcloud`) installed and available in `PATH`.
- `stty` available (standard on Linux/macOS).
- Go 1.23+ only if building from source.

## Build

```bash
make build
```

The binary is written to:

```text
bin/gcloud-env
```

Install it into your Go bin directory with:

```bash
make install
```

Or copy it somewhere in your `PATH`:

```bash
sudo install -m 0755 bin/gcloud-env /usr/local/bin/gcloud-env
```

## Usage

Start the TUI:

```bash
gcloud-env
```

Keys:

| Key | Action |
| --- | --- |
| `↑` / `↓`, `j` / `k` | Move selection |
| `Enter` | Activate selected configuration and restore its saved ADC |
| `a` | Authenticate ADC for the selected configuration and save it |
| `r` | Reload configurations |
| `q`, `Ctrl-C` | Quit |

You can also switch without opening the TUI:

```bash
gcloud-env use minibota
```

And inspect the active context:

```bash
gcloud-env status
```

## First-time setup for each configuration

`gcloud-env` does **not** create or rename your gcloud configurations. It discovers the ones you already have.

For each configuration, do this once:

1. Run `gcloud-env`.
2. Select the configuration.
3. Press `a`.
4. Complete Google's browser authentication flow.

The resulting ADC file is copied into a per-configuration store. From then on, selecting that configuration with `Enter` switches both the normal gcloud context and ADC without another login, until those credentials need to be refreshed.

## Where credentials are stored

By default Google Cloud CLI uses:

```text
~/.config/gcloud/
```

`gcloud-env` stores per-configuration ADC copies under:

```text
~/.config/gcloud/gcloud-env/adc/<configuration>.json
```

When a configuration is activated, its saved copy is atomically restored to:

```text
~/.config/gcloud/application_default_credentials.json
```

If `CLOUDSDK_CONFIG` is set, that directory is used instead of `~/.config/gcloud`.

Credential files and directories are created with restrictive permissions (`0600` for files, `0700` for directories). These files contain credentials: **do not commit them to Git** and do not copy them into the repository.

## Why not `GOOGLE_APPLICATION_CREDENTIALS`?

A child process cannot permanently modify environment variables in its parent shell. A CLI could print an `export ...` command and require `eval`, but that would defeat the goal of switching with one direct command. Restoring the standard ADC file makes the change visible to subsequent tools such as Cloud SQL Auth Proxy without shell integration.

## How switching works

For a configuration called `cosa`, `Enter` effectively does:

```text
gcloud config configurations activate cosa
restore ~/.config/gcloud/gcloud-env/adc/cosa.json
    -> ~/.config/gcloud/application_default_credentials.json
```

If no ADC has been saved for that configuration, the gcloud configuration is still activated and the TUI tells you to press `a` once.

`a` performs the equivalent of:

```text
gcloud auth application-default login ACCOUNT --project=PROJECT
```

and then saves the generated ADC file for that gcloud configuration.

## Development

```bash
make fmt
make test
make build
```

Project layout:

```text
cmd/gcloud-env/       CLI and TUI
internal/gcloud/      gcloud discovery, activation and ADC management
Makefile
README.md
```

## Security notes

This tool intentionally manages local user credential files. It never prints credential contents. Saved ADC files remain on the local machine under the Cloud SDK config directory and are written with user-only permissions.

The ADC associated with a configuration is whatever identity you authenticate when pressing `a`. Before completing the browser flow, verify that the account shown by the TUI is the account you intend to use.

## License

MIT
