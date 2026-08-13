-- CNF-002b: confirmation must defer to unresolved follower reorg suspicion.
ALTER TABLE crawler_state ADD COLUMN reorg_suspected_from INTEGER;
