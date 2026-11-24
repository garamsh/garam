-- cluster_keys.predecessor_id references the table it sits in with ON
-- DELETE CASCADE, so deleting a key, or a cluster's keys with it, reads
-- every row naming it. The index makes that read a lookup (#2057).
CREATE INDEX cluster_keys_predecessor_id_idx ON cluster_keys (predecessor_id);
