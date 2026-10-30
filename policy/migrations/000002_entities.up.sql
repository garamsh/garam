-- The containment and membership decisions read, written by each
-- entity's owner (ADR-0130 §7). A deleted entity keeps its row with no
-- document, so its sequence still refuses an earlier write delivered
-- late (ADR-0138).
CREATE TABLE entities (
    entity_type text NOT NULL,
    entity_id   text NOT NULL,
    sequence    bigint NOT NULL,
    document    jsonb,
    PRIMARY KEY (entity_type, entity_id)
);

-- Every entity one reaches: its parents and the entities its attributes
-- name, so a walk up from a principal or a resource holds each
-- resource's Org and OrgMembers even where parents do not (ADR-0137).
CREATE TABLE entity_references (
    entity_type text NOT NULL,
    entity_id   text NOT NULL,
    target_type text NOT NULL,
    target_id   text NOT NULL,
    PRIMARY KEY (entity_type, entity_id, target_type, target_id),
    FOREIGN KEY (entity_type, entity_id)
        REFERENCES entities (entity_type, entity_id) ON DELETE CASCADE
);
