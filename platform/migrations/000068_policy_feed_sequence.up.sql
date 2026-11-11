-- The one sequence a delivery pass takes its changes' sequence from
-- (ADR-0140 §7). It is shared by every entity: the policy service
-- needs a number that rises with each change to an entity, and a
-- number that rises with every pass does.
CREATE SEQUENCE policy_feed_sequence;
