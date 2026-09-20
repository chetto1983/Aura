#!/usr/bin/env bash
# Intentionally interactive: OTP, organization enrollment and Dashboard rotation have no
# approved credential-free automation path. Never source the appliance's secret .env here.
set -euo pipefail
case "${AURA_E2E_CLOUDFLARE:-0}" in
  0|'') echo 'BLOCKED: named-tunnel live acceptance not opted in; requires a registered domain and operator prerequisites (docs/cloudflare-remote-access.md).' >&2; exit 2 ;;
  1) ;;
  *) echo 'FAIL: AURA_E2E_CLOUDFLARE must be 1 to opt in' >&2; exit 1 ;;
esac
required=(CLOUDFLARE_API_TOKEN CLOUDFLARE_REPLACEMENT_API_TOKEN CLOUDFLARE_ACCOUNT_ID CLOUDFLARE_TEST_ZONE AURA_E2E_ADMIN_EMAIL AURA_E2E_DENIED_EMAIL
  CLOUDFLARE_TEAM_NAME AURA_E2E_ORIGIN AURA_E2E_APPLIANCE_DIR AURA_E2E_UNRELATED_TXT_NAME)
for key in "${required[@]}"; do
  [[ -n "${!key:-}" ]] || { echo "FAIL: opted-in live acceptance requires $key" >&2; exit 1; }
done
[[ "$CLOUDFLARE_ACCOUNT_ID" =~ ^[a-fA-F0-9]{32}$ ]] || { echo 'FAIL: invalid Cloudflare account ID' >&2; exit 1; }
[[ "$AURA_E2E_ORIGIN" == https://* && "$AURA_E2E_ORIGIN" != *\?* && "$AURA_E2E_ORIGIN" != *@* ]] || {
  echo 'FAIL: AURA_E2E_ORIGIN must be a direct HTTPS origin without credentials or query' >&2; exit 1;
}
[[ "${AURA_E2E_CLOUDFLARE_DEDICATED:-}" == 1 ]] || {
  echo 'FAIL: AURA_E2E_CLOUDFLARE_DEDICATED=1 must identify a disposable appliance with no existing integration' >&2; exit 1;
}
[[ "$CLOUDFLARE_TEST_ZONE" == *.* && "$CLOUDFLARE_TEST_ZONE" != *trycloudflare.com && "$CLOUDFLARE_TEST_ZONE" != *://* ]] || {
  echo 'FAIL: a registered test zone is required; Quick Tunnels cannot prove SSE acceptance' >&2; exit 1;
}
[[ -t 0 && -z "${CI:-}" ]] || {
  echo 'FAIL: this live suite requires an attended desktop for OTP, WARP and Dashboard rotation; hermetic CI is a separate gate' >&2; exit 1;
}
[[ -d "$AURA_E2E_APPLIANCE_DIR" && -f "$AURA_E2E_APPLIANCE_DIR/compose.yaml" ]] || {
  echo 'FAIL: dedicated Ubuntu appliance directory/compose.yaml is missing' >&2; exit 1;
}
command -v node >/dev/null
command -v docker >/dev/null
export AURA_E2E_CLOUDFLARE_RUN="aura-e2e-$(date -u +%Y%m%d%H%M%S)-$(openssl rand -hex 3)"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root/web"
# Cleanup is registered in the test before configure. A killed browser/host cannot guarantee
# cleanup: never print success here on failure, and leave the exact hostname for safe recovery.
trap 'code=$?; if ((code != 0)); then echo "FAIL: acceptance incomplete; check Settings > Remote access for $AURA_E2E_CLOUDFLARE_RUN.$CLOUDFLARE_TEST_ZONE and retry its owned-resource deletion" >&2; fi' EXIT
npx playwright test e2e/remote-access-live.spec.ts --project=remote-access-live --workers=1 --retries=0 --headed
