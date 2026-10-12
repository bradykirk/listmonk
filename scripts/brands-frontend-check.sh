#!/usr/bin/env bash
# FORK(hcm): list every line in frontend/src that loads an /api/ URL outside axios
# and fail when a line has no brandURL( and is not allowlisted. Run after each
# upstream sync (see PATCHES.md). Direct browser requests cannot carry the
# X-Listmonk-Brand header, so they must use brandURL() (ext_brand parameter).
set -u
cd "$(dirname "$0")/../frontend/src" || exit 2

# Patterns that load a URL without axios.
PATTERN='document\.location|window\.location|window\.open|:href|:src|action=|new EventSource|uris\.'

# Allowlist: files whose lines are not checked, each with the reason.
#  views/Maintenance.vue: the analytics export is a Global route, not brand scoped.
#  App.vue: uris.root is the site root and uris.errorEvents is the global error log stream.
#  components/CampaignPreview.vue: its lines build previewURL, and the check below
#    requires that previewURL is wrapped in brandURL().
ALLOW='^(\./)?(views/Maintenance\.vue|App\.vue|components/CampaignPreview\.vue):'

# Names in constants.js whose value is an /api/ path. Other uris (static, root) are not API loads.
API_URIS=$(grep -oE "^[[:space:]]*[A-Za-z]+:[[:space:]]*'/api/" constants.js | sed -E "s/^[[:space:]]*([A-Za-z]+):.*/\1/" | paste -sd'|' -)

fail=0

if ! grep -q "brandURL(url)" components/CampaignPreview.vue; then
  echo "FAIL: components/CampaignPreview.vue previewURL must be wrapped in brandURL()"
  fail=1
fi

while IFS= read -r line; do
  file=${line%%:*}
  [[ "$file" == "./api/index.js" ]] && continue
  [[ "$line" =~ $ALLOW ]] && continue
  [[ "$line" == *"brandURL("* ]] && continue
  # Only lines that can reach an /api/ URL.
  if [[ "$line" == *"/api/"* || "$line" =~ uris\.($API_URIS)([^A-Za-z]|$) ]]; then
    echo "FAIL (no brandURL): $line"
    fail=1
  fi
done < <(grep -rnE "$PATTERN" . --include='*.vue' --include='*.js')

if [[ $fail -ne 0 ]]; then
  echo "FAIL: add brandURL() to the lines above, or allowlist them in scripts/brands-frontend-check.sh with a reason."
  exit 1
fi
echo "PASS"
