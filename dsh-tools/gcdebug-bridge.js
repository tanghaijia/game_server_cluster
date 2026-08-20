'use strict';
/**
 * gcdebug-bridge.js — execution bridge for the DSH "gcdebug" debugging plugin.
 *
 * Runs inside a `node -e` subprocess spawned by the dynamic Cordis plugin
 * (`dsh-gcdebug`). The bootstrap snippet evals this file and exposes the
 * request payload as the outer `m` variable (m.request).
 *
 * Supports three request kinds:
 *   { kind: 'http',    url, method, headers, body, timeoutMs }   — HTTP(S) call
 *   { kind: 'pg',      sql, params, limit, readOnly, transaction, commit, db } — SQL
 *   { kind: 'pg_meta', table, sample, db }                       — schema introspection
 *
 * Always writes exactly one JSON object to stdout:
 *   { ok: true,  result: {...} }
 *   { ok: false, error: { message, name?, pgCode?, pgDetail? } }
 *
 * The PostgreSQL client implements the v3 wire protocol directly (no npm
 * dependency): startup, cleartext / MD5 / SCRAM-SHA-256 authentication, and
 * the extended query protocol (Parse/Bind/Describe/Execute/Sync) with
 * text-format parameters and results.
 */

const net = require('node:net');
const crypto = require('node:crypto');

/* ------------------------------------------------------------------ */
/* small binary helpers                                                */
/* ------------------------------------------------------------------ */

const int16 = (n) => {
  const b = Buffer.alloc(2);
  b.writeUInt16BE(n & 0xffff);
  return b;
};
const int32 = (n) => {
  const b = Buffer.alloc(4);
  b.writeUInt32BE(n >>> 0);
  return b;
};
const int32s = (n) => {
  const b = Buffer.alloc(4);
  b.writeInt32BE(n);
  return b;
};
const cstr = (s) => Buffer.concat([Buffer.from(String(s), 'utf8'), Buffer.from([0])]);
const msg = (t, body) => {
  const len = Buffer.alloc(4);
  len.writeUInt32BE((body ? body.length : 0) + 4);
  return Buffer.concat([Buffer.from([t]), len, body || Buffer.alloc(0)]);
};

function pgError(payload, where) {
  let message = '';
  let code = '';
  let detail = '';
  let hint = '';
  let severity = '';
  let off = 0;
  while (off < payload.length) {
    const t = String.fromCharCode(payload[off]);
    off += 1;
    let end = payload.indexOf(0, off);
    if (end === -1) end = payload.length;
    const v = payload.toString('utf8', off, end);
    off = end + 1;
    if (t === 'M') message = v;
    else if (t === 'C') code = v;
    else if (t === 'D') detail = v;
    else if (t === 'H') hint = v;
    else if (t === 'S') severity = v;
  }
  const err = new Error('PostgreSQL ' + where + ' error (' + code + '): ' + message + (detail ? ' -- ' + detail : ''));
  err.pgCode = code;
  err.pgSeverity = severity;
  err.pgDetail = detail;
  err.pgHint = hint;
  return err;
}

/* ------------------------------------------------------------------ */
/* value conversion                                                    */
/* ------------------------------------------------------------------ */

const TYPE_NAMES = {
  16: 'bool',
  20: 'int8',
  21: 'int2',
  23: 'int4',
  25: 'text',
  26: 'oid',
  114: 'json',
  650: 'cidr',
  700: 'float4',
  701: 'float8',
  829: 'macaddr',
  869: 'inet',
  1000: '_bool',
  1007: '_int4',
  1009: '_text',
  1015: '_varchar',
  1016: '_int8',
  1043: 'varchar',
  1082: 'date',
  1114: 'timestamp',
  1184: 'timestamptz',
  1700: 'numeric',
  2950: 'uuid',
  3802: 'jsonb'
};

function typeName(oid) {
  return TYPE_NAMES[oid] || ('oid:' + oid);
}

function convertValue(oid, raw) {
  switch (oid) {
    case 16: // bool
      return raw === 't';
    case 21: // int2
    case 23: // int4
    case 26: // oid
    case 20: { // int8 — number when safe, string otherwise (keep precision)
      const n = Number(raw);
      return Number.isSafeInteger(n) ? n : raw;
    }
    case 700: // float4
    case 701: // float8
    case 1700: { // numeric
      const n = Number(raw);
      return Number.isFinite(n) ? n : raw;
    }
    case 114: // json
    case 3802: { // jsonb
      try {
        return JSON.parse(raw);
      } catch (e) {
        return raw;
      }
    }
    default:
      return raw;
  }
}

/* ------------------------------------------------------------------ */
/* TCP + message framing                                               */
/* ------------------------------------------------------------------ */

function connectPg(host, port, timeoutMs) {
  return new Promise((resolve, reject) => {
    const sock = net.createConnection({ host: host, port: port });
    sock.setNoDelay(true);
    const timer = setTimeout(() => {
      sock.destroy();
      reject(new Error('PG: connect timeout to ' + host + ':' + port));
    }, timeoutMs);
    sock.once('connect', () => {
      clearTimeout(timer);
      resolve(sock);
    });
    sock.once('error', (e) => {
      clearTimeout(timer);
      reject(new Error('PG: connect failed to ' + host + ':' + port + ' -- ' + e.message));
    });
  });
}

/** Single-connection message reader: next() resolves one framed message.
 *  Messages are buffered as they arrive (never dropped), so a burst of
 *  frames (e.g. AuthenticationOk + ParameterStatus… + ReadyForQuery) is
 *  preserved even while no waiter is pending. */
function makeStream(sock) {
  let buf = Buffer.alloc(0);
  const inbox = [];
  const waiters = [];
  let closed = false;
  function deliver(item) {
    if (waiters.length) {
      waiters.shift()(null, item);
    } else {
      inbox.push(item);
    }
  }
  function failAll(err) {
    while (waiters.length) waiters.shift()(err);
  }
  sock.on('data', (chunk) => {
    buf = buf.length === 0 ? chunk : Buffer.concat([buf, chunk]);
    while (buf.length >= 5) {
      // PG message: type(1) + length(4, INCLUDES itself) + payload → total = 1 + len.
      const len = buf.readUInt32BE(1);
      // Old-style startup error: server rejects the startup and replies with
      // type byte + plain text + NUL (no length prefix). Detect it by an
      // implausible length (first length byte non-zero for a <16MB message).
      if (buf[1] !== 0 || buf.length < 1 + len) {
        if (buf.length >= 5 && buf[1] !== 0 && len > 0x100000) {
          const nul = buf.indexOf(0, 1);
          const end = nul === -1 ? buf.length : nul;
          deliver({ type: buf[0], payload: buf.subarray(1, end) });
          buf = buf.subarray(end + (nul === -1 ? 0 : 1));
          continue;
        }
        break;
      }
      deliver({ type: buf[0], payload: buf.subarray(5, 1 + len) });
      buf = buf.subarray(1 + len);
    }
  });
  sock.on('error', (e) => {
    closed = true;
    failAll(e);
  });
  sock.on('close', () => {
    if (closed) return;
    closed = true;
    failAll(new Error('PG: connection closed'));
  });
  return function next(timeoutMs) {
    if (inbox.length) return Promise.resolve(inbox.shift());
    if (closed) return Promise.reject(new Error('PG: connection closed'));
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('PG: read timeout after ' + timeoutMs + 'ms')), timeoutMs);
      waiters.push((err, item) => {
        clearTimeout(timer);
        if (err) reject(err);
        else resolve(item);
      });
    });
  };
}

/* ------------------------------------------------------------------ */
/* authentication (cleartext / MD5 / SCRAM-SHA-256)                    */
/* ------------------------------------------------------------------ */

function scramEscapeName(name) {
  return String(name).replace(/=/g, '=3D').replace(/,/g, '=2C');
}
const hmac = (key, data) => crypto.createHmac('sha256', key).update(data).digest();
const sha256 = (data) => crypto.createHash('sha256').update(data).digest();

async function authenticate(sock, next, cfg) {
  // StartupMessage: protocol 3.0 (196608 = 0x00030000, bytes 00 03 00 00)
  let body = Buffer.from([0, 3, 0, 0]);
  const params = [
    ['user', cfg.user],
    ['database', cfg.database],
    ['client_encoding', 'UTF8'],
    ['application_name', 'dsh-gcdebug']
  ];
  for (const kv of params) body = Buffer.concat([body, cstr(kv[0]), cstr(kv[1])]);
  body = Buffer.concat([body, Buffer.from([0])]);
  const len = Buffer.alloc(4);
  len.writeUInt32BE(body.length + 4);
  sock.write(Buffer.concat([len, body]));

  const RD = 15000;
  let scram = null;
  for (;;) {
    const m = await next(RD);
    if (m.type === 0x45 /* E */) throw pgError(m.payload, 'authentication');
    if (m.type !== 0x52 /* R */) continue; // ParameterStatus / BackendKeyData / Notice
    const code = m.payload.readInt32BE(0);
    if (code === 0) {
      // AuthenticationOk — drain the startup tail (ParameterStatus…,
      // BackendKeyData) up to ReadyForQuery so subsequent queries start from
      // a clean message stream. Buffered messages are preserved by makeStream.
      for (;;) {
        const tail = await next(RD);
        if (tail.type === 0x5a /* Z ReadyForQuery */) return;
      }
    }
    if (code === 3) {
      // cleartext password
      sock.write(msg(0x70, cstr(cfg.password)));
    } else if (code === 5) {
      // MD5 password: md5(md5(password + user) + salt)
      const salt = m.payload.subarray(4, 8);
      const inner = crypto.createHash('md5').update(cfg.password + cfg.user).digest('hex');
      const outer = crypto.createHash('md5').update(inner + salt.toString('binary')).digest('hex');
      sock.write(msg(0x70, cstr('md5' + outer)));
    } else if (code === 10) {
      // SASL — SCRAM-SHA-256
      const mechs = m.payload.subarray(4).toString().split('\0').filter(Boolean);
      if (mechs.indexOf('SCRAM-SHA-256') === -1) {
        throw new Error('PG: unsupported SASL mechanisms: ' + mechs.join(','));
      }
      const nonce = crypto.randomBytes(18).toString('base64').replace(/[^A-Za-z0-9]/g, 'x');
      const gs2 = 'n,,';
      const cfb = 'n=' + scramEscapeName(cfg.user) + ',r=' + nonce;
      const initial = gs2 + cfb;
      const saslBody = Buffer.concat([
        cstr('SCRAM-SHA-256'),
        int32(Buffer.byteLength(initial, 'utf8')),
        Buffer.from(initial, 'utf8')
      ]);
      sock.write(msg(0x70, saslBody));
      scram = { nonce: nonce, cfb: cfb };
    } else if (code === 11) {
      // SASLContinue — server-first-message
      if (!scram) throw new Error('PG: unexpected SASLContinue');
      const serverFirst = m.payload.subarray(4).toString('utf8');
      const r = /r=([^,]+)/.exec(serverFirst);
      const s = /s=([^,]+)/.exec(serverFirst);
      const i = /i=([^,]+)/.exec(serverFirst);
      if (!r || !s || !i) throw new Error('PG: malformed SCRAM server-first: ' + serverFirst);
      if (r[1].indexOf(scram.nonce) !== 0) throw new Error('PG: SCRAM nonce mismatch');
      const saltedPassword = crypto.pbkdf2Sync(cfg.password, Buffer.from(s[1], 'base64'), parseInt(i[1], 10), 32, 'sha256');
      const clientKey = hmac(saltedPassword, 'Client Key');
      const storedKey = sha256(clientKey);
      const clientFinalNoProof = 'c=biws,r=' + r[1];
      const authMessage = scram.cfb + ',' + serverFirst + ',' + clientFinalNoProof;
      const clientSignature = hmac(storedKey, authMessage);
      const proof = Buffer.alloc(32);
      for (let x = 0; x < 32; x++) proof[x] = clientKey[x] ^ clientSignature[x];
      const clientFinal = clientFinalNoProof + ',p=' + proof.toString('base64');
      scram.serverKey = hmac(saltedPassword, 'Server Key');
      scram.authMessage = authMessage;
      sock.write(msg(0x70, Buffer.from(clientFinal, 'utf8')));
    } else if (code === 12) {
      // SASLFinal — verify server signature
      if (!scram) throw new Error('PG: unexpected SASLFinal');
      const serverFinal = m.payload.subarray(4).toString('utf8');
      const v = /v=([^,]+)/.exec(serverFinal);
      if (!v) throw new Error('PG: malformed SCRAM server-final: ' + serverFinal);
      const expected = hmac(scram.serverKey, scram.authMessage).toString('base64');
      if (v[1] !== expected) throw new Error('PG: SCRAM server signature mismatch');
    } else {
      throw new Error('PG: unsupported authentication request code ' + code);
    }
  }
}

/* ------------------------------------------------------------------ */
/* extended query protocol                                             */
/* ------------------------------------------------------------------ */

async function pgRun(sock, next, sql, params) {
  const p = (params || []).map((v) => (v === null || v === undefined ? null : String(v)));

  const parseBody = Buffer.concat([Buffer.from([0]), cstr(sql), int16(0)]);
  const bindParts = [Buffer.from([0]), Buffer.from([0]), int16(0), int16(p.length)];
  for (const v of p) {
    if (v === null) {
      bindParts.push(int32s(-1));
    } else {
      const b = Buffer.from(v, 'utf8');
      bindParts.push(int32(b.length));
      bindParts.push(b);
    }
  }
  bindParts.push(int16(0)); // all result columns in text format
  const bindBody = Buffer.concat(bindParts);
  const describeBody = Buffer.concat([Buffer.from([0x50]), Buffer.from([0])]); // portal
  const executeBody = Buffer.concat([Buffer.from([0]), int32(0)]); // all rows
  sock.write(Buffer.concat([
    msg(0x50, parseBody),
    msg(0x42, bindBody),
    msg(0x44, describeBody),
    msg(0x45, executeBody),
    msg(0x53, Buffer.alloc(0)) // Sync — empty payload (len 4)
  ]));

  const columns = [];
  const types = [];
  const rows = [];
  let command = null;
  let tag = null;
  let error = null;
  const RD = 15000;
  for (;;) {
    const m = await next(RD);
    switch (m.type) {
      case 0x31: // ParseComplete
      case 0x32: // BindComplete
      case 0x74: // ParameterDescription
      case 0x6e: // NoData
      case 0x73: // PortalSuspended
      case 0x53: // ParameterStatus
      case 0x4b: // BackendKeyData
      case 0x4e: // NoticeResponse
        continue;
      case 0x54: { // RowDescription
        const cnt = m.payload.readUInt16BE(0);
        let off = 2;
        for (let x = 0; x < cnt; x++) {
          const end = m.payload.indexOf(0, off);
          const name = m.payload.toString('utf8', off, end);
          off = end + 1;
          off += 4; // table oid
          off += 2; // attribute number
          const typeOid = m.payload.readUInt32BE(off);
          off += 4;
          off += 2; // type size
          off += 4; // type modifier
          off += 2; // format code
          columns.push(name);
          types.push(typeOid);
        }
        break;
      }
      case 0x44: { // DataRow
        const cnt = m.payload.readUInt16BE(0);
        let off = 2;
        const row = {};
        for (let x = 0; x < cnt; x++) {
          const len = m.payload.readInt32BE(off);
          off += 4;
          let val = null;
          if (len >= 0) {
            const raw = m.payload.toString('utf8', off, off + len);
            off += len;
            val = convertValue(types[x], raw);
          }
          row[columns[x] || ('c' + x)] = val;
        }
        rows.push(row);
        break;
      }
      case 0x43: { // CommandComplete
        tag = m.payload.toString('utf8', 0, m.payload.length - 1);
        command = String(tag).split(' ')[0];
        break;
      }
      case 0x49: // EmptyQueryResponse
        break;
      case 0x45: // ErrorResponse
        error = pgError(m.payload, 'query');
        break;
      case 0x5a: // ReadyForQuery
        if (error) throw error;
        return {
          columns: columns,
          types: types.map(typeName),
          rows: rows,
          command: command,
          tag: tag,
          rowCount: rows.length
        };
      default:
        break;
    }
  }
}

/* ------------------------------------------------------------------ */
/* statement classification                                            */
/* ------------------------------------------------------------------ */

function sqlKind(sql) {
  const cleaned = String(sql)
    .replace(/^\s*(--[^\n]*(\n|$)|\/\*[\s\S]*?\*\/|\s)+/g, ' ')
    .trim();
  const m = /^([A-Za-z]+)/.exec(cleaned);
  return m ? m[1].toUpperCase() : '';
}

const READ_ONLY_KINDS = new Set(['SELECT', 'WITH', 'SHOW', 'EXPLAIN', 'VALUES', 'TABLE']);

/* ------------------------------------------------------------------ */
/* request drivers                                                     */
/* ------------------------------------------------------------------ */

function resolveDb(req) {
  const db = (req && req.db) || {};
  return {
    host: db.host || '127.0.0.1',
    port: db.port || 5432,
    user: db.user || 'postgres',
    password: db.password || 'postgres',
    database: db.database || 'game_server',
    schema: db.schema || 'public'
  };
}

function quoteIdent(s) {
  return '"' + String(s).replace(/"/g, '""') + '"';
}

async function doPg(req) {
  const cfg = resolveDb(req);
  const sql = String(req.sql || '').trim();
  if (!sql) throw new Error('empty SQL');
  const kind = sqlKind(sql);
  if (req.readOnly && !READ_ONLY_KINDS.has(kind)) {
    throw new Error('pg_query is read-only; statement kind "' + kind + '" is not allowed. Use pg_exec for writes.');
  }
  const sock = await connectPg(cfg.host, cfg.port, 8000);
  try {
    const next = makeStream(sock);
    await authenticate(sock, next, cfg);
    let result;
    if (req.transaction) {
      await pgRun(sock, next, 'BEGIN', []);
      try {
        result = await pgRun(sock, next, sql, req.params || []);
        await pgRun(sock, next, req.commit === false ? 'ROLLBACK' : 'COMMIT', []);
        result.transaction = req.commit === false ? 'rolled_back' : 'committed';
      } catch (e) {
        try {
          await pgRun(sock, next, 'ROLLBACK', []);
        } catch (e2) {
          /* connection may be dead; ignore */
        }
        throw e;
      }
    } else {
      result = await pgRun(sock, next, sql, req.params || []);
    }
    const limit = req.limit || 200;
    let truncated = false;
    if (result.rows.length > limit) {
      result.rows = result.rows.slice(0, limit);
      truncated = true;
    }
    result.truncated = truncated;
    result.schema = cfg.schema;
    return result;
  } finally {
    sock.end();
  }
}

async function doPgMeta(req) {
  const cfg = resolveDb(req);
  const sock = await connectPg(cfg.host, cfg.port, 8000);
  try {
    const next = makeStream(sock);
    await authenticate(sock, next, cfg);
    const table = req.table ? String(req.table).trim() : '';
    if (!table) {
      const r = await pgRun(
        sock, next,
        'SELECT table_name AS name FROM information_schema.tables WHERE table_schema = $1 ORDER BY table_name',
        [cfg.schema]
      );
      const counts = await pgRun(
        sock, next,
        "SELECT c.relname AS name, c.reltuples::int8 AS approx_rows FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = $1 AND c.relkind = 'r' ORDER BY c.relname",
        [cfg.schema]
      );
      const byName = {};
      for (const row of counts.rows) byName[row.name] = row.approx_rows;
      const tables = r.rows.map((row) => ({ name: row.name, approx_rows: byName[row.name] !== undefined ? byName[row.name] : null }));
      return { kind: 'pg_meta', schema: cfg.schema, tables: tables };
    }
    const cols = await pgRun(
      sock, next,
      'SELECT column_name AS name, data_type AS type, is_nullable AS nullable, column_default AS default_value FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 ORDER BY ordinal_position',
      [cfg.schema, table]
    );
    if (cols.rows.length === 0) {
      throw new Error('table not found in schema "' + cfg.schema + '": ' + table);
    }
    const count = await pgRun(
      sock, next,
      'SELECT count(*)::int8 AS c FROM ' + quoteIdent(cfg.schema) + '.' + quoteIdent(table),
      []
    );
    let sample = null;
    let sampleColumns = null;
    if (req.sample !== false) {
      const s = await pgRun(
        sock, next,
        'SELECT * FROM ' + quoteIdent(cfg.schema) + '.' + quoteIdent(table) + ' LIMIT 5',
        []
      );
      sample = s.rows;
      sampleColumns = s.columns;
    }
    return {
      kind: 'pg_meta',
      schema: cfg.schema,
      table: table,
      columns: cols.rows,
      row_count: count.rows.length ? count.rows[0].c : 0,
      sample: sample,
      sample_columns: sampleColumns
    };
  } finally {
    sock.end();
  }
}

async function doHttp(req) {
  const timeoutMs = req.timeoutMs || 20000;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const res = await fetch(req.url, {
      method: req.method || 'GET',
      headers: req.headers || {},
      body: req.body === undefined || req.body === null
        ? undefined
        : (typeof req.body === 'string' ? req.body : JSON.stringify(req.body)),
      redirect: 'follow',
      signal: controller.signal
    });
    const text = await res.text();
    const MAX = 512 * 1024;
    let bodyText = text;
    let truncated = false;
    if (bodyText.length > MAX) {
      bodyText = bodyText.slice(0, MAX);
      truncated = true;
    }
    let parsed = null;
    let parseError = null;
    if (bodyText.length > 0) {
      try {
        parsed = JSON.parse(bodyText);
      } catch (e) {
        parseError = 'response body is not JSON: ' + e.message;
      }
    }
    const headers = {};
    res.headers.forEach((v, k) => {
      headers[k] = v;
    });
    return {
      kind: 'http',
      status: res.status,
      statusText: res.statusText,
      headers: headers,
      body: parsed,
      bodyText: parsed === null ? bodyText : undefined,
      parseError: parseError,
      truncated: truncated
    };
  } finally {
    clearTimeout(timer);
  }
}

/* ------------------------------------------------------------------ */
/* entry                                                               */
/* ------------------------------------------------------------------ */

(async () => {
  try {
    const req = m.request;
    if (!req || typeof req !== 'object') throw new Error('bridge: missing request');
    let result;
    if (req.kind === 'http') result = await doHttp(req);
    else if (req.kind === 'pg') result = await doPg(req);
    else if (req.kind === 'pg_meta') result = await doPgMeta(req);
    else throw new Error('bridge: unknown request kind: ' + req.kind);
    process.stdout.write(JSON.stringify({ ok: true, result: result }));
  } catch (e) {
    process.stdout.write(JSON.stringify({
      ok: false,
      error: {
        message: String((e && e.message) || e),
        name: e && e.name,
        pgCode: e && e.pgCode,
        pgDetail: e && e.pgDetail
      }
    }));
  }
})();
