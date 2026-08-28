-- ORD-020: record WHICH of the three attribution conditions failed, at the moment
-- the matcher decided. The three cases have different remedies — a wrong-asset send
-- (ORD-002a) is a customer error needing a refund decision, a late send (ORD-002b) may
-- be reattributable, and no-active-order usually means an unsolicited transfer — so
-- "unattributed" alone tells an operator nothing about what to do next.
--
-- It must be stored rather than recomputed on read: by the time anyone looks, the
-- address may have been released, reassigned, or the order expired, so re-running the
-- checks against current state can produce a different answer than the one that was
-- actually made. NULL for every attributed payment, and for rows written before this
-- migration.
ALTER TABLE payments ADD COLUMN unattributed_reason TEXT
    CHECK (unattributed_reason IN ('no_active_order','asset_mismatch','outside_window'));
