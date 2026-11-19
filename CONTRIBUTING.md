# Contributing to Tellwire

Thanks for helping out. Bug reports, fixes, docs and small features are all welcome.

## Getting set up

See [docs/development.md](docs/development.md). You need Go ≥ 1.24 and Node ≥ 22. Docker is optional.

```bash
make test        # Go vet + tests (race), frontend typecheck + unit tests
make e2e         # Playwright end-to-end tests against the mock MUD
```

CI runs the same checks on every pull request, so please make sure they pass locally first.

## Pull requests

- Keep changes focused; open an issue first for anything large or that changes the wire protocol
  ([docs/protocol.md](docs/protocol.md) is normative).
- Add or update tests with your change. Telnet and gateway changes should be covered against the mock MUD.
- Never log terminal text or user input, and never add a way for the browser to choose an upstream address.
- Do not commit credentials, live-server captures or screenshots containing private chat.
- By contributing you agree your work is licensed under the [MIT License](LICENSE).

## Security issues

Please report these privately; see [SECURITY.md](SECURITY.md).
