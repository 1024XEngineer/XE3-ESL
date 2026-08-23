import assert from "node:assert/strict";
import test from "node:test";

test("uses the deployment commit as the stable build ID", async (t) => {
  const original = process.env.NEXT_DEPLOYMENT_ID;
  const commit = "0123456789abcdef0123456789abcdef01234567";
  process.env.NEXT_DEPLOYMENT_ID = commit;
  t.after(() => {
    if (original === undefined) delete process.env.NEXT_DEPLOYMENT_ID;
    else process.env.NEXT_DEPLOYMENT_ID = original;
  });

  const { default: config } = await import("../next.config.mjs?build-id-test");

  assert.equal(config.deploymentId, commit);
  assert.equal(await config.generateBuildId(), commit);
});
