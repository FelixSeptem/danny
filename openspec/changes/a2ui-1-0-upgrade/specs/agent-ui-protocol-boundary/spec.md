# agent-ui-protocol-boundary Specification (Delta)

## MODIFIED Requirements

### Requirement: Dynamic surfaces render only server-registered catalog entries

The system SHALL maintain version-independent application models `DynamicSurface`, `SurfaceAction`, and `SurfaceSubmission`, and a restricted Danny catalog containing only server-registered components and actions. A surface referencing an unregistered component, function, or prop SHALL be rejected at creation. An A2UI 1.0 adapter SHALL render approved surfaces to a wire-compatible format, and the application models SHALL remain independent of A2UI wire versions so a future adapter can be added without changing Agent runtime or business APIs. This change upgrades the only claimed A2UI wire version from v0.9.1 to v1.0 and removes the v0.9.1 wire path entirely; no client or fixture depends on the deprecated version.

#### Scenario: Model-generated component names are not executable

- **WHEN** model output or untrusted content references a component, function, or prop that is not in the registered catalog
- **THEN** surface creation SHALL reject it with a typed error and no renderer SHALL receive an executable definition for it

#### Scenario: Fixture renders with the approved catalog

- **WHEN** the shipped A2UI 1.0 wire-compatible fixture is validated
- **THEN** every referenced component and action SHALL resolve to a registered catalog entry and the fixture SHALL declare `a2ui_version: "1.0"`

### Requirement: Protocol version claims stay verified

The system SHALL document and enforce its protocol version policy: AG-UI compatibility as implemented and fixture-verified, A2UI 1.0 as the only claimed dynamic-surface version, and the 1.0 claim supported only after the 1.0 specification reached a stable release and the React renderer declares and is verified against the real `@a2ui/react` 1.0 package. The compatibility note SHALL record the 1.0 release date, the verified React renderer status, the installed package version, and the conditions that were satisfied to upgrade from v0.9.1.

#### Scenario: No unverified version claim

- **WHEN** the compatibility note or any feature claim is reviewed
- **THEN** it SHALL NOT assert A2UI v1.0 support unless the 1.0 specification is at a stable release, the React renderer declares 1.0 support, and a surface fixture passes against the real `@a2ui/react` 1.0 package; until those conditions are verified it SHALL point to the verification conditions instead of claiming support.

#### Scenario: Version claim is backed by verified release and renderer

- **WHEN** the compatibility note or any feature claim is reviewed
- **THEN** it SHALL assert A2UI 1.0 support only when (a) the specification is at a stable 1.0 release, (b) the React renderer declares 1.0 support, and (c) a surface fixture passes against the real `@a2ui/react` 1.0 package; otherwise it SHALL NOT assert support.
