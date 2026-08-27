const stagingHostname = "staging.speak-up.top";

export function androidDownloadChannelForHost(host) {
  if (typeof host !== "string" || host.trim() === "") return "production";

  try {
    const url = new URL(`http://${host.trim()}`);
    if (
      url.username !== "" ||
      url.password !== "" ||
      url.pathname !== "/" ||
      url.search !== "" ||
      url.hash !== ""
    ) {
      return "production";
    }
    return url.hostname.toLowerCase().replace(/\.$/, "") === stagingHostname
      ? "staging"
      : "production";
  } catch {
    return "production";
  }
}
