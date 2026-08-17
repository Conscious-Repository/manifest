export class PortalError extends Error {
  constructor(
    public readonly status: number,
    public readonly responseBody: string,
  ) {
    super(`AION portal HTTP ${status}: ${responseBody || "request failed"}`);
    this.name = "PortalError";
  }
}

export class PortalClient {
  readonly baseURL: URL;

  constructor(
    baseURL = process.env.AION_PORTAL_URL || "https://portal.aion.bio",
    private readonly token = process.env.AION_PORTAL_TOKEN || "",
  ) {
    this.baseURL = new URL(baseURL.endsWith("/") ? baseURL : `${baseURL}/`);
  }

  async request(path: string, method = "GET", body?: unknown): Promise<unknown> {
    if (!this.token) {
      throw new Error("AION_PORTAL_TOKEN is required. Generate one under api access in the AION portal.");
    }
    const url = new URL(path.replace(/^\//, ""), this.baseURL);
    const response = await fetch(url, {
      method,
      headers: {
        Accept: "application/json",
        Authorization: `Bearer ${this.token}`,
        ...(body === undefined ? {} : { "Content-Type": "application/json" }),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await response.text();
    if (!response.ok) {
      throw new PortalError(response.status, text.trim());
    }
    if (!text.trim()) return { ok: true };
    try {
      return JSON.parse(text) as unknown;
    } catch {
      return text;
    }
  }
}

export function itemPath(item: string): string {
  const segments = item.split("/");
  if (segments.length < 2 || segments.some(segment => !segment || segment === "." || segment === "..")) {
    throw new Error("item id must contain safe slash-delimited segments");
  }
  return segments.map(encodeURIComponent).join("/");
}
