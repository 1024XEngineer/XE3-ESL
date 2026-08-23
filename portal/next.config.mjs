const deploymentId = process.env.NEXT_DEPLOYMENT_ID || undefined;

/** @type {import("next").NextConfig} */
const nextConfig = deploymentId
  ? { deploymentId, generateBuildId: async () => deploymentId }
  : {};

export default nextConfig;
