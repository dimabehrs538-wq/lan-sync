/** 移动端检查更新：读取 GitHub Releases 的最新正式版本（与桌面 latest.json 同一发布），只做版本比较与安装包挑选。 */
export const REPO = "dimabehrs538-wq/lan-sync";
export const RELEASES_URL = `https://github.com/${REPO}/releases/latest`;

export interface MobileRelease { version: string; notes: string; page: string; asset?: { name: string; url: string; size: number } }

/** 语义化版本比较：a>b 返回 1，相等 0，a<b 返回 -1（忽略前缀 v，预发布版本低于正式版） */
export function compareVersions(a: string, b: string): number {
  const parse = (v: string) => {
    const [core, pre = ""] = v.trim().replace(/^v/i, "").split("-", 2);
    return { n: core.split(".").map((x) => parseInt(x, 10) || 0), pre };
  };
  const x = parse(a), y = parse(b);
  for (let i = 0; i < Math.max(x.n.length, y.n.length, 3); i++) {
    const d = (x.n[i] ?? 0) - (y.n[i] ?? 0);
    if (d) return d > 0 ? 1 : -1;
  }
  if (x.pre === y.pre) return 0;
  if (!x.pre) return 1;
  if (!y.pre) return -1;
  return x.pre > y.pre ? 1 : -1;
}

interface GhAsset { name: string; browser_download_url: string; size: number }
interface GhRelease { tag_name: string; body?: string; html_url: string; draft?: boolean; prerelease?: boolean; assets?: GhAsset[] }

/** 从 Release 中挑选本平台的安装包：Android 选通用 APK，iOS 选 IPA（未签名，需自行重签） */
export function parseRelease(r: GhRelease, platform: "android" | "ios"): MobileRelease | null {
  if (!r || r.draft || r.prerelease || !r.tag_name) return null;
  const assets = r.assets ?? [];
  const pick = platform === "android"
    ? assets.find((a) => /\.apk$/i.test(a.name) && /android/i.test(a.name)) ?? assets.find((a) => /\.apk$/i.test(a.name))
    : assets.find((a) => /\.ipa$/i.test(a.name));
  return {
    version: r.tag_name.replace(/^v/i, ""), notes: (r.body ?? "").trim(), page: r.html_url,
    asset: pick ? { name: pick.name, url: pick.browser_download_url, size: pick.size } : undefined,
  };
}

export async function fetchLatest(platform: "android" | "ios", f: typeof fetch = fetch): Promise<MobileRelease | null> {
  const r = await f(`https://api.github.com/repos/${REPO}/releases/latest`, { headers: { Accept: "application/vnd.github+json" } });
  if (!r.ok) throw new Error(`GitHub 返回 HTTP ${r.status}`);
  return parseRelease(await r.json(), platform);
}
