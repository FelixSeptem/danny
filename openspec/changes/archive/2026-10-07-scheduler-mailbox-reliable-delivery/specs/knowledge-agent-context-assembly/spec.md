## ADDED Requirements

### Requirement: Scheduled Agent runs retain occurrence-scoped authority

The system SHALL construct scheduled KnowledgeAgent runs with the persisted schedule occurrence, policy digest, capability digest, projection digest, plan version, and occurrence-scoped effect namespace. Retrieved source and Stele context SHALL remain untrusted data, and a retry SHALL not expand tool authority without an explicit approval boundary.

#### Scenario: Scheduled run carries occurrence identity
- **WHEN** a Baymax mailbox message starts a scheduled Agent run
- **THEN** the Agent request SHALL include the stable occurrence key and the exact schedule policy and capability digests used for admission

#### Scenario: Scheduled retry preserves authority
- **WHEN** a scheduled run resumes after a lease loss or process restart
- **THEN** the resumed run SHALL reuse the persisted plan, projection, capability, and occurrence effect namespace or enter an explicit replan or approval state

#### Scenario: Untrusted scheduled input cannot authorize mutation
- **WHEN** a scheduled instruction or retrieved document asks the Agent to perform an undeclared mutation
- **THEN** server-controlled policy SHALL reject the tool call and SHALL record a policy violation or approval-required outcome

### Requirement: Agent terminal status and delivery status remain separate

The system SHALL record the scheduled Agent terminal result independently from Stele projection and webhook delivery outcomes. A successful Agent run SHALL remain successful when delivery retries or terminal delivery failure occur, and delivery records SHALL retain the occurrence and execution correlations.

#### Scenario: Agent success with pending webhook
- **WHEN** the Agent commits success but a webhook delivery is pending
- **THEN** the execution SHALL remain successful and the delivery SHALL remain independently retryable

#### Scenario: Stele memory replay is correlated to execution
- **WHEN** a scheduled Agent retries a Stele memory write
- **THEN** the adapter SHALL send the same occurrence effect key and SHALL retain the execution and occurrence correlations in durable evidence

#### Scenario: Delivery failure does not authorize a second Agent run
- **WHEN** webhook delivery reaches terminal failure
- **THEN** the application SHALL expose the delivery failure without replaying the Agent execution as an implicit recovery action
