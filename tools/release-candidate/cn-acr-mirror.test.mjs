import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { fileURLToPath } from "node:url";

const workflowFile = fileURLToPath(
  new URL("../../.github/workflows/cn-acr-mirror.yml", import.meta.url),
);

test("CN ACR mirror is manual, Candidate-bound, immutable, and isolated", () => {
  const workflow = readFileSync(workflowFile, "utf8");

  assert.match(workflow, /workflow_dispatch:\n\s+inputs:\n\s+candidate_run_id:/);
  assert.match(workflow, /getWorkflowRun/);
  assert.match(workflow, /run\.name !== "Release Candidate"/);
  assert.match(workflow, /run\.path !== "\.github\/workflows\/release-candidate\.yml"/);
  assert.match(workflow, /run\.event !== "push"/);
  assert.match(workflow, /run\.head_branch !== "main"/);
  assert.match(workflow, /run\.conclusion !== "success"/);
  assert.match(workflow, /manifests\.length !== 1/);
  assert.match(workflow, /quality_run_url == \$run_url/);
  assert.match(workflow, /server_image == \$server_image/);
  assert.match(workflow, /environment:\n\s+name: cn-acr-mirror/);
  assert.match(workflow, /vars\.CN_ACR_REGISTRY/);
  assert.match(workflow, /vars\.CN_ACR_SERVER_REPOSITORY/);
  assert.match(workflow, /secrets\.CN_ACR_USERNAME/);
  assert.match(workflow, /secrets\.CN_ACR_PASSWORD/);
  assert.match(workflow, /\.platform\.os == "linux"/);
  assert.match(workflow, /\.platform\.architecture == "amd64"/);
  assert.doesNotMatch(workflow, /--prefer-index=false/);
  assert.match(workflow, /mirror_tag "\$VERSION"/);
  assert.match(workflow, /mirror_tag "\$CANDIDATE_SHA"/);
  assert.match(workflow, /Refusing to overwrite/);
  assert.match(workflow, /"\$mirrored_platform_digest" != "\$platform_digest"/);
  assert.match(workflow, /version_index_digest.*sha_index_digest/);
  assert.match(workflow, /destination_index_digest/);
  assert.match(workflow, /cn-acr-mirror-receipt\.json/);
  assert.match(workflow, /release_manifest_sha256/);
  assert.doesNotMatch(workflow, /:latest/);
  assert.doesNotMatch(workflow, /staging-deploy\.yml|production-deploy\.yml/);
});
