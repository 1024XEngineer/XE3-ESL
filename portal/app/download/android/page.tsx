import { currentAndroidDownloadChannel } from "../../../lib/android-download-channel-server";
import AndroidDownloadPageClient from "./AndroidDownloadPageClient";

export default async function AndroidDownloadPage() {
  const channel = await currentAndroidDownloadChannel();
  return <AndroidDownloadPageClient channel={channel} />;
}
