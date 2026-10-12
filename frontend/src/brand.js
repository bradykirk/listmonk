// FORK(hcm): the brand scope of this tab. Read once at load; see the brands plan, review round 2.
const KEY = 'ext.brand';

function read() {
  try {
    const tab = sessionStorage.getItem(KEY);
    if (tab !== null) return tab;
    const last = localStorage.getItem(KEY) || '';
    sessionStorage.setItem(KEY, last);
    return last;
  } catch (e) {
    return ''; // storage unavailable: All brands
  }
}

const tabBrand = read();

// The slug of this tab's brand, or '' for All brands.
export const getTabBrand = () => tabBrand;

// Select a brand for this tab and remember it for new tabs, then reload this tab only.
export const selectBrand = (slug) => {
  try {
    sessionStorage.setItem(KEY, slug || '');
    if (slug) { localStorage.setItem(KEY, slug); } else { localStorage.removeItem(KEY); }
  } catch (e) { /* ignore */ }
  window.location.reload();
};

// Add ext_brand to a URL that the browser loads directly (download link,
// iframe src, form action), because those requests cannot carry the header.
export const brandURL = (url) => {
  if (!tabBrand) return url;
  const sep = url.includes('?') ? '&' : '?';
  return `${url}${sep}ext_brand=${encodeURIComponent(tabBrand)}`;
};
