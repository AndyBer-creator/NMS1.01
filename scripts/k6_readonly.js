// Read-only load: /health and /metrics. Requires k6 (https://k6.io/).
// BASE_URL defaults to http://127.0.0.1:8080

import http from "k6/http";
import { check } from "k6";

const base = (__ENV.BASE_URL || "http://127.0.0.1:8080").replace(/\/$/, "");

export const options = {
  vus: Number(__ENV.K6_VUS || 25),
  duration: __ENV.K6_DURATION || "30s",
  thresholds: {
    http_req_failed: ["rate<0.01"],
  },
};

export default function () {
  const path = Math.random() < 0.5 ? "/health" : "/metrics";
  const res = http.get(`${base}${path}`);
  check(res, {
    "status 200": (r) => r.status === 200,
  });
}
