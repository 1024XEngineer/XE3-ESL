import { headers } from "next/headers";
import { androidDownloadChannelForHost } from "./android-download-channel.mjs";

export type AndroidDownloadChannel = "production" | "staging";

export async function currentAndroidDownloadChannel(): Promise<AndroidDownloadChannel> {
  const requestHeaders = await headers();
  return androidDownloadChannelForHost(
    requestHeaders.get("host"),
  ) as AndroidDownloadChannel;
}
