-- `ListAgentDefinitionsByOperator` and `ListOrgsByMember` each filter a
-- column that leads no index, so each read its whole table to answer
-- for one caller. `agent_definitions` is indexed on `org_grn` alone
-- (000010). `org_members` has only its primary key, where `user_grn` is
-- the second column and so cannot be reached by a predicate on it —
-- the same case 000015 answered for `message_consents.org_b`.
--
-- One column each, and it is the predicate. On `agent_definitions` the
-- `ORDER BY` earns no second column: `agent_grn` would remove a sort
-- over rows the one operator already bounds, and being distinct per row
-- it cannot be deduplicated the way the repeating leading column is, so
-- the index grows by an order of magnitude to hold it. On `org_members`
-- the primary key already covers the other direction.
--
-- Each column is also the one a delete reads, because Postgres indexes
-- the referenced side of a foreign key and never the referencing side:
-- 000018's NO ACTION check scans `agent_definitions` for the operator
-- being deleted, and `org_members`' cascade scans it for the user.
CREATE INDEX agent_definitions_operator_idx ON agent_definitions (operator_grn);
CREATE INDEX org_members_user_idx ON org_members (user_grn);
