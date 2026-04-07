# Contributing To Qorvexus

Thanks for contributing.

Qorvexus aims to stay maintainable while growing into a powerful long-running agent platform, so contributions should improve both capability and structure.

## Maintainer

Project maintainer:

- `FWERKOR Team`
- `admin@fwerkor.com`

## Before You Start

- Read [README.md](README.md) for the current product shape.
- Prefer changes that preserve explicit subsystem boundaries instead of pushing more logic into the core runtime.
- If you are adding a channel integration, model provider, or capability extension, favor adapters, tools, skills, or plugins over special cases.

## Development Setup

```bash
go build ./cmd/qorvexus
go test ./...
```

Useful shortcuts:

```bash
make build
make test
make race
make ci
```

## Contribution Guidelines

- Keep repository content in English.
- Prefer small, reviewable pull requests.
- Update docs when behavior, configuration, or user-facing workflows change.
- Add tests for new behavior when practical.
- Do not silently introduce destructive defaults.
- Keep social channels plugin-based.
- Keep provider configuration explicit when it is part of the deployment contract.

## Pull Request Checklist

Before opening a pull request, make sure you have:

- run `go test ./...`
- run `go build ./...`
- updated any affected docs
- added or adjusted tests when behavior changed
- explained the user-facing impact in the PR description

## Design Expectations

Changes are more likely to be accepted if they:

- improve maintainability
- preserve or strengthen trust and permission boundaries
- reduce user-facing complexity without hiding critical operator controls
- fit the existing architecture instead of bypassing it

## Reporting Bugs And Requesting Features

- Use GitHub Issues for normal bugs and feature requests.
- Use email for security-sensitive reports: `admin@fwerkor.com`

## Licensing

By contributing to this repository, you agree that your contributions will be licensed under the repository license.
