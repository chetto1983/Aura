-- pg_restore installs schema objects with a restricted search_path. Keep the
-- extension function and dictionary resolvable while generated columns invoke
-- this wrapper during a restore into an empty database.
CREATE OR REPLACE FUNCTION aura.searchable_text(source text)
RETURNS text
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
AS $$
    SELECT coalesce(source, '') || ' ' ||
           regexp_replace(coalesce(source, ''), '[^[:alnum:]]+', ' ', 'g') || ' ' ||
           public.unaccent('public.unaccent'::regdictionary, coalesce(source, '')) || ' ' ||
           regexp_replace(
               public.unaccent('public.unaccent'::regdictionary, coalesce(source, '')),
               '[^[:alnum:]]+', ' ', 'g')
$$;
