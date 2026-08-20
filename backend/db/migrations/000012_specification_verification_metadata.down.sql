ALTER TABLE specification_acceptance_criteria
    DROP COLUMN IF EXISTS verified_at,
    DROP COLUMN IF EXISTS verified_by;

ALTER TABLE specification_reproduction_steps
    DROP COLUMN IF EXISTS verified_at,
    DROP COLUMN IF EXISTS verified_by;
