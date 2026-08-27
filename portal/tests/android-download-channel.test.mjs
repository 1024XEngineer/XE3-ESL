import assert from "node:assert/strict";
import test from "node:test";

import { androidDownloadChannelForHost } from "../lib/android-download-channel.mjs";

test("selects Staging only for the exact Staging portal host", () => {
  assert.equal(androidDownloadChannelForHost("staging.speak-up.top"), "staging");
  assert.equal(
    androidDownloadChannelForHost("STAGING.SPEAK-UP.TOP:443"),
    "staging",
  );
  assert.equal(androidDownloadChannelForHost("staging.speak-up.top."), "staging");

  for (const host of [
    null,
    "",
    "speak-up.top",
    "staging.speak-up.top.example.com",
    "staging.speak-up.top@example.com",
    "staging.speak-up.top/path",
  ]) {
    assert.equal(androidDownloadChannelForHost(host), "production");
  }
});
