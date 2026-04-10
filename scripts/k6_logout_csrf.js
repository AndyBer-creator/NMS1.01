// Session + CSRF (mutating): Basic auth, GET /devices (cookie nms_csrf), POST /logout + X-CSRF-Token.
// Требуются k6 и работающий API.
//
// ENV:
// - BASE_URL (default http://127.0.0.1:8080)
// - K6_VUS, K6_DURATION
// - K6_VIEWER_USER / K6_VIEWER_PASS (или NMS_VIEWER_USER / NMS_VIEWER_PASS)

import http from "k6/http";
import { check } from "k6";
import exec from "k6/execution";
import encoding from "k6/encoding";

const base = (__ENV.BASE_URL || "http://127.0.0.1:8080").replace(/\/$/, "");

export const options = {
  vus: Number(__ENV.K6_VUS || 10),
  duration: __ENV.K6_DURATION || "30s",
  thresholds: {
    http_req_failed: ["rate<0.01"],
    checks: ["rate>0.99"],
  },
};

function viewerCreds() {
  const u = (__ENV.K6_VIEWER_USER || __ENV.NMS_VIEWER_USER || "").trim();
  const p = (__ENV.K6_VIEWER_PASS || __ENV.NMS_VIEWER_PASS || "").trim();
  return { u, p };
}

function basicAuthHeader(u, p) {
  return `Basic ${encoding.b64encode(`${u}:${p}`)}`;
}

function snippet(s, max) {
  const v = String(s || "");
  if (v.length <= max) {
    return v;
  }
  return v.slice(0, max) + "...";
}

function csrfFromResponse(res, pageURL) {
  const list = res.cookies && res.cookies["nms_csrf"];
  if (list && list.length > 0) {
    const first = list[0];
    const v = first.value !== undefined ? first.value : first;
    if (v) {
      return String(v);
    }
  }
  const jar = http.cookieJar();
  const fromJar = jar.cookiesForURL(pageURL);
  if (fromJar && fromJar["nms_csrf"] && fromJar["nms_csrf"].length > 0) {
    return String(fromJar["nms_csrf"][0]);
  }
  const sc = res.headers["Set-Cookie"] || res.headers["set-cookie"] || "";
  const joined = Array.isArray(sc) ? sc.join(";") : String(sc);
  const m = joined.match(/nms_csrf=([^;]+)/);
  return m ? decodeURIComponent(m[1]) : "";
}

export function setup() {
  const { u, p } = viewerCreds();
  if (!u || !p || u === "..." || p === "...") {
    exec.test.abort(
      "Set K6_VIEWER_USER and K6_VIEWER_PASS (Basic auth viewer). " +
        "Обычно те же значения, что NMS_VIEWER_USER / NMS_VIEWER_PASS в .env (передайте в окружение k6: export ...)."
    );
  }

  const res = http.get(`${base}/devices`, {
    headers: {
      Accept: "application/json",
      Authorization: basicAuthHeader(u, p),
    },
  });
  if (res.status !== 200) {
    exec.test.abort(
      `Viewer seed failed: GET /devices expected 200, got ${res.status}. Body=${JSON.stringify(
        snippet(res.body, 200)
      )}`
    );
  }
}

export default function () {
  const { u, p } = viewerCreds();
  const authz = basicAuthHeader(u, p);
  const seedURL = `${base}/devices`;

  const getRes = http.get(seedURL, {
    headers: {
      Accept: "application/json",
      Authorization: authz,
    },
  });

  check(getRes, {
    "GET /devices 200": (r) => r.status === 200,
  });
  if (getRes.status !== 200) {
    return;
  }

  const token = csrfFromResponse(getRes, seedURL);
  const postRes = http.post(`${base}/logout`, "", {
    redirects: 0,
    headers: {
      "X-CSRF-Token": token,
      Authorization: authz,
    },
  });

  check(postRes, {
    "POST /logout 302": (r) => r.status === 302,
    "POST /logout Location /login": (r) =>
      String(r.headers.Location || "").startsWith("/login"),
  });
}

