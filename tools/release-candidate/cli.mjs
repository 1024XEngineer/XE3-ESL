export function readArguments(arguments_, allowedNames, usage) {
  const allowed = new Set(allowedNames);
  const values = {};
  for (let index = 0; index < arguments_.length; index += 2) {
    const flag = arguments_[index];
    const value = arguments_[index + 1];
    if (
      !flag?.startsWith("--") ||
      value === undefined ||
      value.startsWith("--")
    ) {
      throw new Error(usage);
    }
    const name = flag.slice(2);
    if (!allowed.has(name)) {
      throw new Error(`Unknown option: ${flag}`);
    }
    if (Object.hasOwn(values, name)) {
      throw new Error(`Option may only be provided once: ${flag}`);
    }
    values[name] = value;
  }
  return values;
}
