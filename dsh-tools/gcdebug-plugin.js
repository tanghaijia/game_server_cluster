/**
 * ============================================================================
 * gcdebug — game-server-cluster 调试工具（DSH 动态插件）定义存档
 * ============================================================================
 *
 * 背景：DSH 动态插件是进程内存级的。DSH 进程重启后，插件注册（gdbg-*）会丢失，
 *       但本文件与 dsh-tools/gcdebug-bridge.js 都在仓库磁盘上，可随时重放恢复。
 *
 * 恢复步骤（在新会话 / 新进程中由 agent 执行，约 10 秒）：
 *   1. 用 read 工具读取本文件，取出下方 HOST_CODE（String.raw 模板字符串的内容，
 *      即"return { ... }"整段函数体）。
 *   2. 调用 cordis_define：
 *        plugin: { kind: 'new', idPrefix: 'gdbg' }
 *        name:   'game-cluster-debug'
 *        purpose:'为 DSH 提供 controller-go HTTP API 调用与 PostgreSQL 观察/编辑的调试工具集'
 *        code:   { host: <HOST_CODE 内容> }
 *   3. 用返回的 pluginId / packageId 调用 cordis_run（mode: 'run'）激活。
 *   4. 验证：模型工具集出现 controller_api / pg_query / pg_exec / pg_meta 四个工具。
 *
 * 依赖：
 *   - 桥接文件 dsh-tools/gcdebug-bridge.js（插件运行时按会话工作区实时读取，已入库）。
 *   - 默认连接（本机部署）：controller API http://127.0.0.1:8088；
 *     数据库 myuser@127.0.0.1:5432/cluster_game_server_db（schema controller）。
 *   - 若换环境，可用工具参数的 db / base_url 覆盖，或直接改下方 DB_DEFAULTS / API_DEFAULT。
 *
 * 修改桥接逻辑：编辑 gcdebug-bridge.js 后重启插件（cordis_run 同包重跑）即可，无需改本文件。
 * ============================================================================
 */

const HOST_CODE = String.raw`return {
  inject: ['subprocess', 'fs', 'workspaceRegistry'],
  apply(ctx) {
    // node -e 引导：从 stdin 读取 { script, request }，eval 桥接脚本（桥接脚本为纯 Node，无 npm 依赖）
    const BOOTSTRAP = "let d='';process.stdin.on('data',c=>d+=c);process.stdin.on('end',()=>{const m=JSON.parse(d);eval(Buffer.from(m.script,'base64').toString())})";
    const BRIDGE_REL = 'dsh-tools/gcdebug-bridge.js';
    const API_DEFAULT = 'http://127.0.0.1:8088';
    const DB_DEFAULTS = { host: '127.0.0.1', port: 5432, user: 'myuser', password: 'mysecretpassword', database: 'cluster_game_server_db', schema: 'controller' };

    // 解析会话工作区根目录（sandboxPolicy.workspaceRoot 是用户主目录，不是会话工作区；
    // workspaceRegistry 里的 workspace.path 才是真实工作区），并确认桥接文件可读。
    let rootPromise = null;
    function resolveRoot() {
      if (!rootPromise) {
        rootPromise = (async () => {
          const list = await ctx.workspaceRegistry.list();
          for (const w of list) {
            const p = w && w.path;
            if (typeof p !== 'string' || !p) continue;
            try {
              const t = await ctx.fs.resolve(BRIDGE_REL, { cwd: p });
              await ctx.fs.readText(t);
              return p;
            } catch (e) {
              // 该工作区不包含桥接文件，尝试下一个
            }
          }
          return null;
        })();
      }
      return rootPromise;
    }

    let bridgeB64Promise = null;
    function bridgeB64() {
      if (!bridgeB64Promise) {
        bridgeB64Promise = (async () => {
          const root = await resolveRoot();
          if (!root) throw new Error('bridge file not found under any workspace: ' + BRIDGE_REL);
          const target = await ctx.fs.resolve(BRIDGE_REL, { cwd: root });
          const text = await ctx.fs.readText(target);
          return { root: root, b64: btoa(text) };
        })();
      }
      return bridgeB64Promise;
    }

    async function runBridge(request, signal) {
      try {
        const { root, b64 } = await bridgeB64();
        const payload = JSON.stringify({ script: b64, request: request });
        const handle = ctx.subprocess.spawn({
          argv: ['node', '-e', BOOTSTRAP],
          cwd: root,
          stdio: {
            stdin: { data: payload },
            stdout: { maxBytes: 8 * 1024 * 1024, spill: { maxBytes: 64 * 1024 * 1024 } },
            stderr: { maxBytes: 1024 * 1024, spill: { maxBytes: 16 * 1024 * 1024 } }
          },
          graceMs: 2000,
          signal: signal || undefined
        });
        const outcome = await handle.done;
        const out = handle.collected.stdout.readFrom(0);
        const text = (out.text || '').trim();
        if (text) {
          let parsed;
          try {
            parsed = JSON.parse(text);
          } catch (e) {
            return { ok: false, error: { message: 'bridge returned invalid JSON', detail: text.slice(0, 1000) } };
          }
          if (parsed && parsed.ok === true) {
            return { ok: true, ...(parsed.result || {}) };
          }
          return { ok: false, error: (parsed && parsed.error) || { message: 'bridge error' } };
        }
        const err = handle.collected.stderr.readFrom(0);
        return { ok: false, error: { message: 'bridge process exited ' + (outcome ? outcome.exitCode : '?'), stderr: (err.text || '').slice(0, 1000) } };
      } catch (e) {
        return { ok: false, error: { message: 'bridge spawn failed: ' + String((e && e.message) || e) } };
      }
    }

    function mergeDb(over) {
      const o = over || {};
      const out = {};
      for (const k of Object.keys(DB_DEFAULTS)) out[k] = DB_DEFAULTS[k];
      for (const k of Object.keys(o)) if (o[k] !== undefined && o[k] !== null) out[k] = o[k];
      return out;
    }

    function makeTool(name, description, parameters, run) {
      return harness.defineTool({
        name: name,
        description: description,
        parameters: parameters,
        output: {
          schema: { type: 'json' },
          render(args, value) {
            return [{ type: 'text', text: JSON.stringify(value, null, 2) }];
          }
        },
        timeoutMs: 60000,
        execute(args, exec) {
          return run(args, exec);
        }
      });
    }

    const controllerApi = makeTool(
      'controller_api',
      '调用 controller-go 的 HTTP 接口（controller-go/docs/api.md）。' +
      '基础地址默认 http://127.0.0.1:8088（本机实际部署；可用 base_url 覆盖）。' +
      '常用接口：GET /healthz、GET /debug/version、GET /debug/instances（实例聚合视图）、' +
      'GET /api/games、GET /api/game-instances?status=running、GET /api/nodes、GET /api/node-agents/health、' +
      'GET /api/observe/nodes、GET /api/observe/queue、GET /api/observe/events、GET /api/observe/scheduler/stats、' +
      'GET /debug/reconcile；写操作：POST /api/games、POST /api/game-instances、' +
      'POST /api/game-instances/{id}/start|stop|cancel|retry|dispatch、PUT /api/nodes/{id} 等。' +
      '返回 { ok, status, body }，非 2xx 也视为成功返回（含错误信息）。',
      {
        type: 'object',
        properties: {
          method: { type: 'string', description: 'HTTP 方法', enum: ['GET', 'POST', 'PUT', 'DELETE', 'PATCH'] },
          path: { type: 'string', description: '接口路径，支持 {param} 占位替换，如 /api/game-instances/{id}/start' },
          path_params: { type: 'object', description: '路径参数，如 {"id":"inst-xxx"}' },
          query: { type: 'object', description: 'URL 查询参数，如 {"status":"running"}' },
          body: { description: 'JSON 请求体（对象自动序列化）' },
          headers: { type: 'object', description: '额外请求头' },
          base_url: { type: 'string', description: '基础地址，默认 http://127.0.0.1:8088' },
          timeout_ms: { type: 'integer', description: '超时毫秒，默认 20000' }
        },
        required: ['path']
      },
      async (args, exec) => {
        const method = String(args.method || 'GET').toUpperCase();
        let path = String(args.path || '').trim();
        if (!path) return { ok: false, error: { message: 'path is required' } };
        if (!path.startsWith('/')) path = '/' + path;
        const base = String(args.base_url || API_DEFAULT).replace(/\/+$/, '');
        let url = base + path;
        const pp = args.path_params || {};
        for (const k of Object.keys(pp)) {
          url = url.split('{' + k + '}').join(encodeURIComponent(String(pp[k])));
        }
        const q = args.query || {};
        const qs = Object.keys(q).map((k) => encodeURIComponent(k) + '=' + encodeURIComponent(String(q[k]))).join('&');
        if (qs) url += (url.indexOf('?') >= 0 ? '&' : '?') + qs;
        const headers = {};
        const h = args.headers || {};
        for (const k of Object.keys(h)) headers[k] = String(h[k]);
        let body;
        if (args.body !== undefined && args.body !== null) {
          body = typeof args.body === 'string' ? args.body : JSON.stringify(args.body);
          if (!Object.keys(headers).some((k) => k.toLowerCase() === 'content-type')) {
            headers['Content-Type'] = 'application/json';
          }
        }
        const out = await runBridge({ kind: 'http', url: url, method: method, headers: headers, body: body, timeoutMs: args.timeout_ms || 20000 }, exec.signal);
        return { tool: 'controller_api', method: method, url: url, ...out };
      }
    );

    const pgQuery = makeTool(
      'pg_query',
      '对 controller 的 PostgreSQL 执行只读 SQL（SELECT/WITH/SHOW/EXPLAIN/VALUES/TABLE），返回 { ok, columns, types, rows, rowCount }。' +
      '默认连接 myuser@127.0.0.1:5432/cluster_game_server_db（schema=controller，可用 db 参数覆盖 host/port/user/password/database/schema）。' +
      '注意：controller 本地表为读路径权威，例如 games、game_instances、nodes、node_agents、game_container_configs、' +
      'game_container_port_mappings、steam_branches、scheduling_queue、scheduler_events、node_resource_samples。' +
      '写语句会被拒绝（28P01 之外的错误会提示改用 pg_exec）。',
      {
        type: 'object',
        properties: {
          sql: { type: 'string', description: '只读 SQL 语句' },
          params: { type: 'array', description: '参数（$1/$2...），按顺序对应', items: { description: '参数值' } },
          limit: { type: 'integer', description: '最大返回行数，默认 200' },
          db: { type: 'object', description: '连接覆盖 {host,port,user,password,database,schema}' }
        },
        required: ['sql']
      },
      async (args, exec) => {
        const out = await runBridge({ kind: 'pg', readOnly: true, sql: String(args.sql || ''), params: args.params || [], limit: args.limit || 200, db: mergeDb(args.db) }, exec.signal);
        return { tool: 'pg_query', ...out };
      }
    );

    const pgExec = makeTool(
      'pg_exec',
      '对 controller 的 PostgreSQL 执行写 SQL（INSERT/UPDATE/DELETE/ALTER/CREATE/DROP 等），在事务中执行：' +
      'commit=true（默认）提交，commit=false 回滚（试运行，安全验证写语句效果）。' +
      '返回 { ok, command, tag, rowCount, transaction }。默认连接同 pg_query。' +
      '注意：直接改库可能绕过 controller 的状态机/调度逻辑，优先使用 controller_api 的正式接口。',
      {
        type: 'object',
        properties: {
          sql: { type: 'string', description: '写 SQL 语句' },
          params: { type: 'array', description: '参数（$1/$2...）', items: { description: '参数值' } },
          commit: { type: 'boolean', description: 'true=提交（默认）；false=回滚试运行' },
          db: { type: 'object', description: '连接覆盖 {host,port,user,password,database,schema}' }
        },
        required: ['sql']
      },
      async (args, exec) => {
        const out = await runBridge({ kind: 'pg', readOnly: false, transaction: true, commit: args.commit !== false, sql: String(args.sql || ''), params: args.params || [], db: mergeDb(args.db) }, exec.signal);
        return { tool: 'pg_exec', ...out };
      }
    );

    const pgMeta = makeTool(
      'pg_meta',
      '探查 PostgreSQL 数据库结构：不传 table 列出所有表（含估算行数）；传 table 返回列定义、总行数和样例数据。' +
      '用于观察/编辑前了解库结构。默认连接同 pg_query（cluster_game_server_db / schema controller）。',
      {
        type: 'object',
        properties: {
          table: { type: 'string', description: '表名；省略则列出所有表' },
          sample: { type: 'boolean', description: '是否附带样例数据，默认 true' },
          db: { type: 'object', description: '连接覆盖 {host,port,user,password,database,schema}' }
        }
      },
      async (args, exec) => {
        const out = await runBridge({ kind: 'pg_meta', table: String(args.table || ''), sample: args.sample !== false, db: mergeDb(args.db) }, exec.signal);
        return { tool: 'pg_meta', ...out };
      }
    );

    ctx.effect(() => {
      const d1 = harness.registerTool(ctx, controllerApi);
      const d2 = harness.registerTool(ctx, pgQuery);
      const d3 = harness.registerTool(ctx, pgExec);
      const d4 = harness.registerTool(ctx, pgMeta);
      return () => { d1(); d2(); d3(); d4(); };
    });
  }
};`;

module.exports = {
  HOST_CODE,
  idPrefix: 'gdbg',
  name: 'game-cluster-debug',
  purpose: '为 DSH 提供 controller-go HTTP API 调用与 PostgreSQL 观察/编辑的调试工具集',
  bridgeFile: 'dsh-tools/gcdebug-bridge.js'
};
