-- Deterministic operator profile (migration 0097). It is written by onboarding and the
-- profile editor, and read on the turn path -- so the reads are narrow on purpose.

-- name: UpsertIdentityProfile :one
INSERT INTO aura.identity_profiles (
    identity_id, display_name, role, company, location, timezone, lang,
    tone_preference, response_length, custom_instructions,
    voice_mode, can_proactive_message,
    expertise, stack, projects, goals, interests, people, vetoes
) VALUES (
    sqlc.arg(identity_id), sqlc.arg(display_name), sqlc.arg(role), sqlc.arg(company),
    sqlc.arg(location), sqlc.arg(timezone), sqlc.arg(lang), sqlc.arg(tone_preference),
    sqlc.arg(response_length), sqlc.arg(custom_instructions),
    sqlc.narg(voice_mode), sqlc.narg(can_proactive_message),
    sqlc.arg(expertise), sqlc.arg(stack), sqlc.arg(projects), sqlc.arg(goals),
    sqlc.arg(interests), sqlc.arg(people), sqlc.arg(vetoes)
)
ON CONFLICT (identity_id) DO UPDATE SET
    -- NULLIF on every scalar: the form submits the fields it collected, and a later save
    -- that leaves one blank must not erase an answer an earlier one provided. The editor
    -- clears a field by other means, never by omission.
    display_name        = COALESCE(NULLIF(EXCLUDED.display_name, ''), aura.identity_profiles.display_name),
    role                = COALESCE(NULLIF(EXCLUDED.role, ''), aura.identity_profiles.role),
    company             = COALESCE(NULLIF(EXCLUDED.company, ''), aura.identity_profiles.company),
    location            = COALESCE(NULLIF(EXCLUDED.location, ''), aura.identity_profiles.location),
    timezone            = COALESCE(NULLIF(EXCLUDED.timezone, ''), aura.identity_profiles.timezone),
    lang                = COALESCE(NULLIF(EXCLUDED.lang, ''), aura.identity_profiles.lang),
    tone_preference     = COALESCE(NULLIF(EXCLUDED.tone_preference, ''), aura.identity_profiles.tone_preference),
    response_length     = COALESCE(NULLIF(EXCLUDED.response_length, ''), aura.identity_profiles.response_length),
    custom_instructions = COALESCE(NULLIF(EXCLUDED.custom_instructions, ''), aura.identity_profiles.custom_instructions),
    voice_mode            = COALESCE(EXCLUDED.voice_mode, aura.identity_profiles.voice_mode),
    can_proactive_message = COALESCE(EXCLUDED.can_proactive_message, aura.identity_profiles.can_proactive_message),
    expertise = CASE WHEN cardinality(EXCLUDED.expertise) > 0 THEN EXCLUDED.expertise ELSE aura.identity_profiles.expertise END,
    stack     = CASE WHEN cardinality(EXCLUDED.stack)     > 0 THEN EXCLUDED.stack     ELSE aura.identity_profiles.stack END,
    projects  = CASE WHEN cardinality(EXCLUDED.projects)  > 0 THEN EXCLUDED.projects  ELSE aura.identity_profiles.projects END,
    goals     = CASE WHEN cardinality(EXCLUDED.goals)     > 0 THEN EXCLUDED.goals     ELSE aura.identity_profiles.goals END,
    interests = CASE WHEN cardinality(EXCLUDED.interests) > 0 THEN EXCLUDED.interests ELSE aura.identity_profiles.interests END,
    people    = CASE WHEN cardinality(EXCLUDED.people)    > 0 THEN EXCLUDED.people    ELSE aura.identity_profiles.people END,
    vetoes    = CASE WHEN cardinality(EXCLUDED.vetoes)    > 0 THEN EXCLUDED.vetoes    ELSE aura.identity_profiles.vetoes END,
    updated_at = now()
RETURNING *;

-- The editor's write. It REPLACES every field, because the editor renders every field: a
-- value arriving empty there means the operator cleared it, and the merge above would make
-- that impossible -- you could add a veto and never remove one. The onboarding seed keeps
-- the merge; the editor owns the whole row.
-- name: ReplaceIdentityProfile :one
INSERT INTO aura.identity_profiles (
    identity_id, display_name, role, company, location, timezone, lang,
    tone_preference, response_length, custom_instructions,
    voice_mode, can_proactive_message,
    expertise, stack, projects, goals, interests, people, vetoes
) VALUES (
    sqlc.arg(identity_id), sqlc.arg(display_name), sqlc.arg(role), sqlc.arg(company),
    sqlc.arg(location), sqlc.arg(timezone), sqlc.arg(lang), sqlc.arg(tone_preference),
    sqlc.arg(response_length), sqlc.arg(custom_instructions),
    sqlc.narg(voice_mode), sqlc.narg(can_proactive_message),
    sqlc.arg(expertise), sqlc.arg(stack), sqlc.arg(projects), sqlc.arg(goals),
    sqlc.arg(interests), sqlc.arg(people), sqlc.arg(vetoes)
)
ON CONFLICT (identity_id) DO UPDATE SET
    display_name        = EXCLUDED.display_name,
    role                = EXCLUDED.role,
    company             = EXCLUDED.company,
    location            = EXCLUDED.location,
    timezone            = EXCLUDED.timezone,
    lang                = EXCLUDED.lang,
    tone_preference     = EXCLUDED.tone_preference,
    response_length     = EXCLUDED.response_length,
    custom_instructions = EXCLUDED.custom_instructions,
    voice_mode            = EXCLUDED.voice_mode,
    can_proactive_message = EXCLUDED.can_proactive_message,
    expertise = EXCLUDED.expertise,
    stack     = EXCLUDED.stack,
    projects  = EXCLUDED.projects,
    goals     = EXCLUDED.goals,
    interests = EXCLUDED.interests,
    people    = EXCLUDED.people,
    vetoes    = EXCLUDED.vetoes,
    updated_at = now()
RETURNING *;

-- name: GetIdentityProfile :one
SELECT * FROM aura.identity_profiles WHERE identity_id = sqlc.arg(identity_id);

-- The turn path wants one string, not a row: the clock is rendered on every request.
-- name: GetIdentityTimezone :one
SELECT timezone FROM aura.identity_profiles WHERE identity_id = sqlc.arg(identity_id);

-- The onboarding gate. Two timestamps rather than a state enum: they say WHEN, which a
-- boolean cannot, and "completed" plus "skipped" are not mutually exclusive over time --
-- an operator may skip today and fill the form next week.
-- name: MarkOnboardingCompleted :exec
INSERT INTO aura.identity_profiles (identity_id, completed_at)
VALUES (sqlc.arg(identity_id), now())
ON CONFLICT (identity_id) DO UPDATE SET completed_at = now(), updated_at = now();

-- name: MarkOnboardingSkipped :exec
INSERT INTO aura.identity_profiles (identity_id, skipped_at)
VALUES (sqlc.arg(identity_id), now())
ON CONFLICT (identity_id) DO UPDATE SET skipped_at = now(), updated_at = now();

-- The timestamps themselves, not two booleans: `IS NOT NULL AS completed` comes back
-- untyped from Postgres and sqlc renders it interface{}, and the answered/unanswered
-- question is exactly what a nullable timestamp already encodes.
-- name: MarkOnboardingNudged :exec
INSERT INTO aura.identity_profiles (identity_id, seed_nudged_at)
VALUES (sqlc.arg(identity_id), now())
ON CONFLICT (identity_id) DO UPDATE SET seed_nudged_at = now(), updated_at = now();

-- name: GetOnboardingState :one
SELECT completed_at, skipped_at, seed_nudged_at
FROM aura.identity_profiles
WHERE identity_id = sqlc.arg(identity_id);
