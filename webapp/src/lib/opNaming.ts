// Accept PascalCase operation names from older transport callers.
export function methodToOp(method: string): string {
  return method.replace(/([a-z0-9])([A-Z])/g, "$1-$2").toLowerCase();
}

/** Convert a generated CSIL service name such as BoardService to the mock route key. */
export function serviceToWire(service: string): string {
  return service.replace(/Service$/, "").toLowerCase();
}
