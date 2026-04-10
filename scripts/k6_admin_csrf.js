// Admin-only + CSRF: Basic admin, GET /devices (cookie nms_csrf), POST /devices с пустым JSON-телом.
// Валидация возвращает 400 «IP, Name, Community required» — запись в БД не выполняется.
//
// Важно: k6 по умолчанию считает HTTP 400 «failed»; в этом скрипте нет порога http_req_failed — только checks.
//
// ENV:
// - BASE_URL (default http://127.0.0.1:8080)
// - K6_VUS, K6_DURATION
// - K6_ADMIN_USER / K6_ADMIN_PASS (или NMS_ADMIN_USER / NMS_ADMIN_PASS)

import http from "k6/http";
import { check } from "k6";
import exec from "k6/execution";
import encoding from "k6/encoding";

const base = (__ENV.BASE_URL || "http://127.0.0.1:8080").replace(/\/$/, "");

export const options = {
  vus: Number(__ENV.K6_VUS || 5),
  duration: __ENV.K6_DURATION || "30s",
  thresholds: {
    checks: ["rate>0.99"],
  },
};

function adminCreds() {
  const u = (__ENV.K6_ADMIN_USER || __ENV.NMS_ADMIN_USER || "").trim();
  const p = (__ENV.K6_ADMIN_PASS || __ENV.NMS_ADMIN_PASS || "").trim();
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
  const { u, p } = adminCreds();
  if (!u || !p || u === "..." || p === "...") {
    exec.test.abort(
      "Set K6_ADMIN_USER and K6_ADMIN_PASS (Basic auth admin). " +
        "Обычно те же значения, что NMS_ADMIN_USER / NMS_ADMIN_PASS в .env (export для k6)."
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
      `Admin seed failed: GET /devices expected 200, got ${res.status}. Body=${JSON.stringify(
        snippet(res.body, 200)
      )}`
    );
  }
}

export default function () {
  const { u, p } = adminCreds();
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
  const postRes = http.post(`${base}/devices`, "{}", {
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
      "X-CSRF-Token": token,
      Authorization: authz,
    },
  });

  check(postRes, {
    "POST /devices 400 validation": (r) => r.status === 400,
    "POST /devices body mentions required": (r) =>
      String(r.body).toLowerCase().includes("required"),
  });
}
