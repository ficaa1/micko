# Security

## Reporting a vulnerability

Report it privately through
[GitHub's security advisories](https://github.com/ficaa1/micko/security/advisories/new),
not in a public issue. Include the output of `micko --version`, your Argo
Workflows version and the steps to reproduce.

Fixes go into the latest release.

## How micko handles credentials

- The bearer token comes from an environment variable (`tokenEnv`) or a file
  (`tokenFile`, `--token-file`). micko reads it on every request, sends it
  only in the Authorization header and never puts it in a URL, an error or
  a log. Startup errors name the missing
  setting, never its value. Keep literal tokens out of command lines and
  committed configs.
- micko connects only to the Argo Server in your profile. With port-forward
  it runs `kubectl port-forward` against your kubeconfig context and stops
  it on exit.
- HTTPS certificates are verified. `caFile` adds a private CA, and
  `--insecure-skip-tls-verify` turns verification off for a single run with
  a warning. Plain HTTP is allowed only to loopback addresses, and redirects
  are refused.
- What an action may do is decided by the Argo Server and the token's
  permissions. Every action asks first, and `--read-only` turns them off for
  a session. `--demo` uses no cluster and no credentials.
- Parameter and output values are shown by default; `redactValues` or
  `--redact-values` hides them. Logs, copied text and pipe output can still
  contain whatever your workflows print.
- `--debug` output is sanitized, and token-shaped text is masked.
