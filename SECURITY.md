# Security Policy

## Supported Versions

Qorvexus is currently maintained on the `main` branch.

Security fixes are expected to land on:

- the latest commit on `main`
- the latest tagged release line, when release branches exist in the future

If you are running an older fork or snapshot, please upgrade before requesting support.

## Reporting A Vulnerability

Please do **not** open public GitHub issues for security-sensitive reports.

Send reports to:

- `admin@fwerkor.com`

Please include:

- a clear description of the issue
- reproduction steps or a minimal proof of concept
- affected configuration, plugin, or deployment context
- impact assessment if known
- any suggested mitigation

## Response Expectations

The FWERKOR Team will try to:

- acknowledge receipt within 5 business days
- assess severity and reproduction status
- coordinate a fix or mitigation path
- publish a patch or advisory when appropriate

## Scope

High-priority security issues typically include:

- arbitrary command execution bypasses
- privilege boundary failures between owner / trusted / external contexts
- credential leakage
- unsafe self-modification flows
- unauthorized outbound messaging or impersonation
- remote code execution through plugins, tools, or model adapters

Lower-priority issues may still be accepted, but triage time can vary.

## Disclosure Guidance

Please avoid:

- public disclosure before maintainers have time to investigate
- mass scanning or destructive testing against infrastructure you do not own
- sharing private keys, real user data, or production secrets in reports

If a report contains sensitive material, note that explicitly in the email subject line.
