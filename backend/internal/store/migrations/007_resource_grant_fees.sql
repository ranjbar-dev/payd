-- WDR-023a/WDR-025: retain and ledger resource-transaction network fees.
ALTER TABLE resource_grants ADD COLUMN fee_raw TEXT;
