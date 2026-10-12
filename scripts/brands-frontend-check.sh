#!/usr/bin/env bash
# FORK(hcm): list every line in frontend/src that loads an /api/ URL outside axios
# and fail when a line has no brandURL( and is not allowlisted. Run after each
# upstream sync (see PATCHES.md). Direct browser requests cannot carry the
# X-Listmonk-Brand header, so they must use brandURL() (ext_brand parameter).
#
# LIMIT: the script reads lines. It cannot see a URL that is built in a computed
# property or a variable and loaded on another line. Review such code by hand
# (CampaignPreview.vue previewURL is the known case; see the check below).
set -u
cd "${BRANDS_CHECK_SRC:-$(dirname "$0")/../frontend/src}" || exit 2

# Patterns that load a URL without axios.
PATTERN='document\.location|window\.location|window\.open|:href|:src|action=|new EventSource|fetch\(|XMLHttpRequest|uris\.'

# Allowlist: exact "file|line content" (leading space trimmed). A changed or new
# line in the same file is NOT exempt.
ALLOW=(
  # Maintenance.vue: the analytics export is a Global route, not brand scoped.
  'views/Maintenance.vue|:href="exportURL">'
  # App.vue: uris.root is the site root, not an API load.
  'App.vue|document.location.href = uris.root;'
  # App.vue: the global error log stream, not brand scoped.
  'App.vue|const evtSource = new EventSource(uris.errorEvents, { withCredentials: true });'
  # CampaignPreview.vue: these lines use previewURL, a computed that is wrapped in
  # brandURL() (verified by the check below) and the uris it is built from.
  'components/CampaignPreview.vue|<form v-if="isPost" method="post" :action="previewURL" target="iframe" ref="form">'
  'components/CampaignPreview.vue|<iframe id="iframe" name="iframe" ref="iframe" :title="title" :src="isPost ? '"'about:blank'"' : previewURL"'
  'components/CampaignPreview.vue|uri = this.isArchive ? uris.previewCampaignArchive : uris.previewCampaign;'
  'components/CampaignPreview.vue|uri = uris.previewTemplate;'
  'components/CampaignPreview.vue|uri = uris.previewRawTemplate;'
)

# Names in constants.js whose value is an /api/ path. Other uris (static, root) are not API loads.
API_URIS=$(grep -oE "^[[:space:]]*[A-Za-z]+:[[:space:]]*'/api/" constants.js | sed -E "s/^[[:space:]]*([A-Za-z]+):.*/\1/" | paste -sd'|' -)

fail=0

if ! grep -q "return url === 'about:blank' ? url : brandURL(url);" components/CampaignPreview.vue; then
  echo "FAIL: components/CampaignPreview.vue previewURL must return brandURL(url)"
  fail=1
fi

while IFS= read -r line; do
  file=${line%%:*}; file=${file#./}
  rest=${line#*:}; rest=${rest#*:}
  content=$(printf '%s' "$rest" | sed -E 's/^[[:space:]]+//; s/[[:space:]]+$//')
  [[ "$file" == "api/index.js" ]] && continue
  [[ "$content" == *"brandURL("* ]] && continue
  allowed=0
  for a in "${ALLOW[@]}"; do
    [[ "$a" == "$file|$content" ]] && { allowed=1; break; }
  done
  [[ $allowed -eq 1 ]] && continue
  # Only lines that can reach an /api/ URL.
  if [[ "$content" == *"/api/"* || "$content" == *"previewURL"* || "$content" == *"exportURL"* \
        || "$content" =~ uris\.($API_URIS)([^A-Za-z]|$) ]]; then
    echo "FAIL (no brandURL): $file: $content"
    fail=1
  fi
done < <(grep -rnE "$PATTERN" . --include='*.vue' --include='*.js')

if [[ $fail -ne 0 ]]; then
  echo "FAIL: add brandURL() to the lines above, or allowlist the exact line in scripts/brands-frontend-check.sh with a reason."
  exit 1
fi
echo "PASS"
