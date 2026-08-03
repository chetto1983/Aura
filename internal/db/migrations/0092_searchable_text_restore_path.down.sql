CREATE OR REPLACE FUNCTION aura.searchable_text(source text)
RETURNS text
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
AS $$
    SELECT coalesce(source, '') || ' ' ||
           regexp_replace(coalesce(source, ''), '[^[:alnum:]]+', ' ', 'g') || ' ' ||
           unaccent('unaccent'::regdictionary, coalesce(source, '')) || ' ' ||
           regexp_replace(
               unaccent('unaccent'::regdictionary, coalesce(source, '')),
               '[^[:alnum:]]+', ' ', 'g')
$$;
