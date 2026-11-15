.pragma library

// Pure label logic for the triage window: capability gate, catalog merging
// by name across accounts, the "/" tree, chips, picker diffs and view
// parameters. No QML types here so node can test it.
//
// A catalog entry is a LabelView from labels.list:
//   {id, accountId, name, colorBg, colorText, monitored}

// ---- capability -----------------------------------------------------------

// THE single place that decides whether an account supports labels. The
// daemon exposes the capability through accounts.list; if the field name
// differs, fix this one function.
function accountHasLabels(account) {
    if (!account)
        return false;
    if (Array.isArray(account.capabilities))
        return account.capabilities.indexOf("labels") !== -1;
    if (typeof account.labels === "boolean")
        return account.labels;
    return account.type === "gmail";
}

function labelAccountIds(accounts) {
    var ids = {};
    (accounts || []).forEach(function(a) { if (accountHasLabels(a)) ids[a.id] = true; });
    return ids;
}

// Keep only entries of label-capable accounts.
function gateCatalog(catalog, accounts) {
    var ok = labelAccountIds(accounts);
    return (catalog || []).filter(function(l) { return ok[l.accountId] === true; });
}

// ---- names and merging ----------------------------------------------------

function leafOf(name) {
    var i = String(name).lastIndexOf("/");
    return i < 0 ? String(name) : String(name).slice(i + 1);
}

function parentOf(name) {
    var i = String(name).lastIndexOf("/");
    return i < 0 ? "" : String(name).slice(0, i);
}

function ancestorsOf(name) {
    var parts = String(name).split("/"), out = [];
    for (var i = 1; i < parts.length; i++)
        out.push(parts.slice(0, i).join("/"));
    return out;
}

function byName(a, b) {
    var x = a.toLowerCase(), y = b.toLowerCase();
    if (x < y) return -1;
    if (x > y) return 1;
    return a < b ? -1 : a > b ? 1 : 0;
}

// One entry per exact name: {name, leaf, accounts: {accountId: entry},
// colorBg, colorText (first account that has a color), monitored (any)}.
function mergeByName(catalog) {
    var map = {}, order = [];
    (catalog || []).forEach(function(l) {
        var m = map[l.name];
        if (!m) {
            m = map[l.name] = {name: l.name, leaf: leafOf(l.name), accounts: {}, colorBg: "", colorText: "", monitored: false};
            order.push(l.name);
        }
        m.accounts[l.accountId] = l;
        if (!m.colorBg && l.colorBg) {
            m.colorBg = l.colorBg;
            m.colorText = l.colorText || "";
        }
        if (l.monitored) m.monitored = true;
    });
    order.sort(byName);
    return order.map(function(n) { return map[n]; });
}

// Nested tree. A parent without a label of its own is selectable:false.
function buildTree(merged) {
    var nodes = {}, roots = [];
    function node(path) {
        if (nodes[path]) return nodes[path];
        var n = nodes[path] = {name: path, leaf: leafOf(path), selectable: false, children: [],
                               colorBg: "", colorText: "", accounts: {}};
        var p = parentOf(path);
        if (p === "") roots.push(n); else node(p).children.push(n);
        return n;
    }
    merged.forEach(function(m) {
        var n = node(m.name);
        n.selectable = true;
        n.colorBg = m.colorBg;
        n.colorText = m.colorText;
        n.accounts = m.accounts;
    });
    function sort(list) {
        list.sort(function(a, b) { return byName(a.leaf, b.leaf); });
        list.forEach(function(n) { sort(n.children); });
    }
    sort(roots);
    return roots;
}

// Visible rows of the tree for a ListView: {name, leaf, depth, hasChildren,
// expanded, selectable, colorBg, colorText}. `expanded` is {name: true}.
function flatten(tree, expanded) {
    var out = [];
    function walk(list, depth) {
        list.forEach(function(n) {
            var open = n.children.length > 0 && expanded[n.name] === true;
            out.push({name: n.name, leaf: n.leaf, depth: depth, hasChildren: n.children.length > 0,
                      expanded: open, selectable: n.selectable, colorBg: n.colorBg, colorText: n.colorText});
            if (open) walk(n.children, depth + 1);
        });
    }
    walk(tree, 0);
    return out;
}

function treeRows(catalog, expanded) {
    return flatten(buildTree(mergeByName(catalog)), expanded || {});
}

function nameExists(catalog, name) {
    return (catalog || []).some(function(l) { return l.name === name; });
}

// ---- per-thread resolution ------------------------------------------------

var _idxCatalog = null, _idx = null;
function index(catalog) {
    if (catalog === _idxCatalog && _idx) return _idx;
    var m = {};
    (catalog || []).forEach(function(l) { m[l.accountId + "\u0000" + l.id] = l; });
    _idxCatalog = catalog;
    _idx = m;
    return m;
}

// Catalog entries (user labels only; system IDs are not in the catalog) the
// thread carries, sorted by name.
function threadLabels(catalog, thread) {
    if (!thread || !thread.labels || thread.labels.length === 0) return [];
    var idx = index(catalog), out = [];
    thread.labels.forEach(function(id) {
        var l = idx[thread.accountId + "\u0000" + id];
        if (l) out.push(l);
    });
    out.sort(function(a, b) { return byName(a.name, b.name); });
    return out;
}

// Chips for a row/header: at most `max` chips plus a "+N" remainder.
function threadChips(catalog, thread, max) {
    var all = threadLabels(catalog, thread);
    var shown = all.slice(0, max), rest = all.slice(max);
    return {
        chips: shown.map(function(l) {
            return {id: l.id, name: l.name, leaf: leafOf(l.name), colorBg: l.colorBg || "", colorText: l.colorText || ""};
        }),
        more: rest.length,
        moreNames: rest.map(function(l) { return l.name; })
    };
}

// Label ID a name resolves to in one account ("" when it has none).
function idForName(catalog, accountId, name) {
    var found = "";
    (catalog || []).forEach(function(l) {
        if (l.accountId === accountId && l.name === name) found = l.id;
    });
    return found;
}

// Params that scope threads.list / threads.fetchLabel to a label view. With
// an account filter and a label that account has, use the (account + label ID)
// form; otherwise the name form that the daemon resolves per account.
function viewParams(catalog, name, accountId) {
    if (accountId) {
        var id = idForName(catalog, accountId, name);
        if (id !== "") return {account: accountId, label: id};
    }
    return {labelName: name};
}

// ---- picker ---------------------------------------------------------------

// Which names are set on the whole selection: a name is set only when every
// selected thread carries the label of that name in its account.
function pickerState(catalog, threads) {
    var merged = mergeByName(catalog), set = {};
    merged.forEach(function(m) {
        set[m.name] = threads.length > 0 && threads.every(function(t) {
            var l = m.accounts[t.accountId];
            return !!l && (t.labels || []).indexOf(l.id) !== -1;
        });
    });
    return {merged: merged, set: set};
}

// Rows for the picker. The create row sits first when nothing matches the
// text, last when something matches but no label has exactly that name.
function pickerRows(merged, text) {
    var q = String(text || "").trim(), ql = q.toLowerCase();
    var rows = merged.filter(function(m) { return ql === "" || m.name.toLowerCase().indexOf(ql) !== -1; })
        .map(function(m) { return {kind: "label", name: m.name, entry: m}; });
    if (q !== "") {
        var exact = merged.some(function(m) { return m.name.toLowerCase() === ql; });
        if (!exact) {
            var row = {kind: "create", name: q};
            if (rows.length === 0) rows.unshift(row); else rows.push(row);
        }
    }
    return rows;
}

// Names to add/remove given the initial and desired maps ({name: bool}).
function pickerChanges(initial, desired) {
    var add = [], remove = [];
    Object.keys(desired).forEach(function(n) {
        if (desired[n] === (initial[n] === true)) return;
        if (desired[n]) add.push(n); else remove.push(n);
    });
    add.sort(byName);
    remove.sort(byName);
    return {add: add, remove: remove};
}

// Accounts of the selected threads that must get a new label of this name
// before it can be applied (they support labels and lack the name).
function accountsNeedingLabel(catalog, accounts, threads, name) {
    var ok = labelAccountIds(accounts), seen = {}, out = [];
    threads.forEach(function(t) {
        if (!ok[t.accountId] || seen[t.accountId]) return;
        seen[t.accountId] = true;
        if (idForName(catalog, t.accountId, name) === "") out.push(t.accountId);
    });
    return out;
}

// ---- settings -------------------------------------------------------------

function accountLabels(catalog, accountId) {
    return (catalog || []).filter(function(l) { return l.accountId === accountId; })
        .sort(function(a, b) { return byName(a.name, b.name); });
}

// Labels nested under `name` in the same account (for the rename hint).
function childCount(catalog, accountId, name) {
    return accountLabels(catalog, accountId).filter(function(l) { return l.name.indexOf(name + "/") === 0; }).length;
}

// New always-synced ID list for one account after flipping one label.
function monitoredAfter(catalog, accountId, labelId, on) {
    var ids = accountLabels(catalog, accountId).filter(function(l) {
        return l.id === labelId ? on : l.monitored;
    }).map(function(l) { return l.id; });
    return ids;
}

function withMonitored(catalog, accountId, labelId, on) {
    return (catalog || []).map(function(l) {
        if (l.accountId !== accountId || l.id !== labelId) return l;
        return Object.assign({}, l, {monitored: on});
    });
}

function samePair(a, b) {
    return String(a.background || "").toLowerCase() === String(b.background || "").toLowerCase()
        && String(a.text || "").toLowerCase() === String(b.text || "").toLowerCase();
}
