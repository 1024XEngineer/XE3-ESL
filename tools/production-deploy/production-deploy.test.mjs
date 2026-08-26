import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { fileURLToPath } from "node:url";

const workflow = readFileSync(
  fileURLToPath(
    new URL("../../.github/workflows/production-deploy.yml", import.meta.url),
  ),
  "utf8",
);

test("Production deploy is gated by the successful official Staging workflow", () => {
  assert.match(
    workflow,
    /workflow_run:\n\s+workflows:\n\s+- Deploy Staging Candidate/,
  );
  assert.match(workflow, /github\.event\.workflow_run\.conclusion == 'success'/);
  assert.match(workflow, /STAGING_HEAD_REPOSITORY.*head_repository\.full_name/);
  assert.match(workflow, /STAGING_PATH" != "\.github\/workflows\/staging-deploy\.yml"/);
  assert.match(workflow, /STAGING_EVENT" != "workflow_run"/);
  assert.match(workflow, /STAGING_HEAD_BRANCH" != "main"/);
  assert.match(workflow, /1024XEngineer\/XE3-ESL/);
  assert.match(workflow, /receipt_version == 2/);
  assert.match(workflow, /deployment_run_id == \$run_id/);
  assert.match(workflow, /deployment_run_attempt == \$run_attempt/);
});

test("Production deploy consumes one matching Candidate without rebuilding it", () => {
  assert.match(workflow, /getWorkflowRun/);
  assert.match(workflow, /run\.name !== "Release Candidate"/);
  assert.match(workflow, /run\.event !== "push"/);
  assert.match(workflow, /run\.head_branch !== "main"/);
  assert.match(workflow, /run\.head_sha !== process\.env\.CANDIDATE_SHA/);
  assert.match(workflow, /speakup-v\$\{process\.env\.VERSION\}-release-manifest/);
  assert.match(workflow, /speakup-v\$\{process\.env\.VERSION\}-android/);
  assert.match(workflow, /Candidate run \$\{runId\} has \$\{selected\.length\}/);
  assert.match(workflow, /run-id:.*candidate_run_id/);
  assert.match(workflow, /tools\/android-download\/bundle\.mjs/);
  assert.match(workflow, /production_apk_sha256/);
  assert.match(workflow, /staging_receipt_sha256/);
  assert.doesNotMatch(workflow, /flutter build|build-android|docker\/build-push-action/);
});

test("Production mutation waits for Environment approval and uses only the broker", () => {
  assert.match(workflow, /environment:\n\s+name: production/);
  assert.match(
    workflow,
    /group: production-deployment\n\s+cancel-in-progress: false/,
  );
  assert.match(workflow, /DEPLOY_USER" == "speakup-production-ci"/);
  assert.match(workflow, /StrictHostKeyChecking=yes/);
  assert.match(workflow, /tar --format=ustar --no-recursion/);
  assert.match(workflow, /action:\s*"inspect"/);
  assert.match(workflow, /action:\s*"deploy"/);
  assert.match(workflow, /action:\s*"release"/);
  assert.match(workflow, /expected_current_receipt_sha256/);
  assert.match(workflow, /production_engine_previous_receipt_sha256/);
  assert.doesNotMatch(workflow, /deploy\/production\/manage\.sh/);
  assert.match(workflow, /permissions:\n\s+actions: read\n\s+contents: write/);
  assert.doesNotMatch(workflow, /packages:\s*write/);
});

test("Production deploy preserves auditable evidence and performs public smoke", () => {
  assert.match(workflow, /https:\/\/speak-up\.top\//);
  assert.match(workflow, /https:\/\/api\.speak-up\.top\/health/);
  assert.match(
    workflow,
    /production-deployment-\$\{\{ github\.run_id \}\}-\$\{\{ github\.run_attempt \}\}/,
  );
  assert.match(workflow, /retention-days: 90/);
  assert.match(workflow, /Remove the Production SSH identity/);
});

test("Production finalization is ordered, complete, immutable, and retryable", () => {
  assert.match(workflow, /tools\/release-finalize\/notes\.mjs/);
  assert.match(workflow, /--published-at "\$PUBLISHED_AT"/);
  assert.match(workflow, /Prepare the complete GitHub Release draft/);
  assert.match(workflow, /gh release upload "\$tag"/);
  assert.match(workflow, /Activate the approved APK through the restricted broker/);
  assert.match(workflow, /Verify the public APK and changelog contract/);
  assert.match(workflow, /Publish and verify the immutable GitHub Release/);
  assert.match(workflow, /\.immutable == true/);
  assert.match(workflow, /\.receipt\.deployment_run_attempt[\s\S]*<= \$deployment_run_attempt/);

  const draft = workflow.indexOf("Prepare the complete GitHub Release draft");
  const activate = workflow.indexOf("Activate the approved APK");
  const publish = workflow.indexOf("Publish and verify the immutable GitHub Release");
  assert.ok(draft > 0 && draft < activate && activate < publish);
});

test("Every external action is pinned to a full commit", () => {
  const references = [...workflow.matchAll(/uses:\s+([^\s#]+)/g)].map(
    ([, reference]) => reference,
  );
  assert.ok(references.length >= 7);
  for (const reference of references) {
    assert.match(reference, /@[0-9a-f]{40}$/, reference);
  }
});
