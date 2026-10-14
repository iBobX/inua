// inua workshop's helper for ES modules: the same as inua.js.
//   const { app, ui } = await import(process.env.INUA_WORKSHOP_LIB + "/node/inua.mjs");
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const inua = require("./inua.js");

export const { app, ui, InuaError, App } = inua;
export default inua;
