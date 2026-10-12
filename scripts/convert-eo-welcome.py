"""Convert the EmailOctopus TA welcome email to Listmonk tags (spec section 9).

Usage: python3 -I scripts/convert-eo-welcome.py IN OUT

Fails when an EmailOctopus {{Tag}} or the text "EmailOctopus" is left.
"""
import re, sys
src, dst = sys.argv[1], sys.argv[2]
html = open(src, encoding="utf-8").read()
# Remove the EmailOctopus footer link (it is the only use of RewardsURL).
html = re.sub(r'<a[^>]*\{\{RewardsURL\}\}[^>]*>.*?</a>', '', html, flags=re.S)
repl = {"{{UnsubscribeURL}}": "{{ UnsubscribeURL }}", "{{WebVersionURL}}": "{{ MessageURL }}",
        "{{SenderInfo}}": "[[SENDER_INFO]]", "{{PreviewText}}": ""}
for k, v in repl.items():
    html = html.replace(k, v)
left = re.findall(r'\{\{[A-Za-z]+\}\}', html)
if left or "EmailOctopus" in html:
    sys.exit(f"FAIL: EmailOctopus content left: {sorted(set(left))} {'EmailOctopus' if 'EmailOctopus' in html else ''}")
open(dst, "w", encoding="utf-8").write(html)
print("OK", dst)
