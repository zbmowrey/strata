# Security policy

## Supported versions

Strata is pre-1.0. Security fixes land on `main` and ship in the next patch release of the latest
minor version. Older releases are not patched.

| Version | Supported |
|---|---|
| 0.1.x (latest) | ✅ |
| older | ❌ |

## Reporting a vulnerability

Please **do not open a public issue** for a security problem.

Report it privately through GitHub's
[private vulnerability reporting](https://github.com/zbmowrey/strata/security/advisories/new).
Include what you found, how to reproduce it, the affected version (`strata version`), and the impact
you expect.

What to expect:

- An acknowledgement within **5 business days**.
- An assessment and, if confirmed, a fix plan within **30 days**. Complex issues may take longer;
  you'll get status updates in the advisory thread.
- Credit in the advisory and the CHANGELOG, unless you prefer to stay anonymous.

## Scope

In scope: the `strata` engine (`engine/`), the Claude Code plugin (`plugin/`), the files `strata init`
writes into a repository (hooks, permission allowlists, managed blocks), and the release artifacts.

Areas of particular interest:

- Hooks or generated permission allowlists that let an agent run commands beyond a script's declared
  side-effect class.
- Content from the knowledge base, inbox or external sources being treated as instructions rather
  than data.
- The local-only private tier being committed, gardened or sent anywhere.
- Secrets reaching shards, project logs, telemetry or garden PRs.

Out of scope: vulnerabilities in Claude Code itself (report those to Anthropic), and findings that
require an attacker who can already write to your repository.
