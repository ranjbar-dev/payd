-- BAL-001 / WDR-023: index owned recipients without scanning every payment.
CREATE INDEX idx_payments_to_address
    ON payments(to_address, asset)
    WHERE direction = 'out';
