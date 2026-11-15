const {test} = require('node:test');
const assertStrict = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const source = fs.readFileSync(path.join(__dirname,'../../quickshell/Common/Labels.js'),'utf8');
const L = vm.runInNewContext(source.replace(/^\.pragma library\s*/, '')+
 '\n({accountHasLabels,gateCatalog,leafOf,parentOf,ancestorsOf,mergeByName,buildTree,flatten,treeRows,threadChips,threadLabels,idForName,viewParams,pickerState,pickerRows,pickerChanges,accountsNeedingLabel,accountLabels,childCount,monitoredAfter,withMonitored,nameExists,samePair})');
const j = x => JSON.parse(JSON.stringify(x));
// Values built inside the vm context are cross-realm: normalize before deepEqual.
const assert = Object.assign(Object.create(assertStrict), {
  deepEqual: (a, b, m) => assertStrict.deepEqual(j(a), j(b), m),
  equal: assertStrict.equal, ok: assertStrict.ok,
});
const lab = (id, accountId, name, extra) => Object.assign({id, accountId, name, colorBg:'', colorText:'', monitored:false}, extra||{});
const catalog = [
  lab('L1','a','Auto/Compras',{colorBg:'#16a765',colorText:'#ffffff'}),
  lab('L2','b','Auto/Compras'),
  lab('L3','a','Trabajo'),
  lab('L4','b','Trabajo/Hoy',{monitored:true}),
  lab('L5','a','auto/zeta'),
];

test('capability helper reads capabilities, a boolean, or falls back to gmail', () => {
  assert.equal(L.accountHasLabels({capabilities:['labels']}), true);
  assert.equal(L.accountHasLabels({capabilities:['archive']}), false);
  assert.equal(L.accountHasLabels({labels:false, type:'gmail'}), false);
  assert.equal(L.accountHasLabels({type:'gmail'}), true);
  assert.equal(L.accountHasLabels({type:'imap'}), false);
  assert.equal(L.accountHasLabels(null), false);
  const g = L.gateCatalog(catalog, [{id:'a',type:'gmail'},{id:'b',type:'imap'}]);
  assert.deepEqual(g.map(l => l.id), ['L1','L3','L5']);
});

test('names split on slash', () => {
  assert.equal(L.leafOf('Auto/Compras'), 'Compras');
  assert.equal(L.parentOf('A/B/C'), 'A/B');
  assert.equal(L.parentOf('A'), '');
  assert.deepEqual(L.ancestorsOf('A/B/C'), ['A','A/B']);
});

test('merge by exact name across accounts keeps per-account IDs and the first color', () => {
  const m = L.mergeByName(catalog);
  const c = m.find(x => x.name === 'Auto/Compras');
  assert.deepEqual(Object.keys(c.accounts).sort(), ['a','b']);
  assert.equal(c.accounts.b.id, 'L2');
  assert.equal(c.colorBg, '#16a765');
  assert.equal(c.colorText, '#ffffff');
  assert.equal(m.find(x => x.name === 'Trabajo/Hoy').monitored, true);
  assert.equal(m.length, 4); // exact names: auto/zeta differs from Auto/...
});

test('tree nests by slash and marks missing parents non-selectable', () => {
  const t = L.buildTree(L.mergeByName(catalog));
  assert.deepEqual(t.map(n => n.name), ['Auto','auto','Trabajo']);
  const auto = t.find(n => n.name === 'Auto');
  assert.equal(auto.selectable, false);
  assert.deepEqual(auto.children.map(n => n.name), ['Auto/Compras']);
  const trabajo = t.find(n => n.name === 'Trabajo');
  assert.equal(trabajo.selectable, true);
  assert.equal(trabajo.children[0].name, 'Trabajo/Hoy');
  // 'auto' (lowercase parent of auto/zeta) is its own non-selectable node
  assert.ok(t.find(n => n.name === 'auto'));
});

test('flatten honors the expanded set and depth', () => {
  const closed = L.treeRows(catalog, {});
  assert.deepEqual(closed.map(r => r.name), ['Auto','auto','Trabajo']);
  const open = L.treeRows(catalog, {Auto:true, Trabajo:true});
  assert.deepEqual(open.map(r => [r.name, r.depth]), [['Auto',0],['Auto/Compras',1],['auto',0],['Trabajo',0],['Trabajo/Hoy',1]]);
  assert.equal(open.find(r => r.name === 'Auto').expanded, true);
  assert.equal(open.find(r => r.name === 'Auto').selectable, false);
});

test('chips resolve IDs per account, drop system labels, sort, and collapse to +N', () => {
  const t = {accountId:'a', labels:['INBOX','L3','L1','L5','STARRED','L2']}; // L2 belongs to account b
  const r = L.threadChips(catalog, t, 2);
  assert.deepEqual(r.chips.map(c => c.name), ['Auto/Compras','auto/zeta']);
  assert.equal(r.chips[0].leaf, 'Compras');
  assert.equal(r.chips[0].colorBg, '#16a765');
  assert.equal(r.more, 1);
  assert.deepEqual(r.moreNames, ['Trabajo']);
  assert.deepEqual(L.threadChips(catalog, {accountId:'a', labels:['INBOX']}, 3), {chips:[], more:0, moreNames:[]});
  assert.deepEqual(L.threadChips(catalog, {accountId:'a'}, 3).chips, []);
});

test('name resolves to the label ID of each account; view params', () => {
  assert.equal(L.idForName(catalog, 'b', 'Auto/Compras'), 'L2');
  assert.equal(L.idForName(catalog, 'b', 'Nope'), '');
  assert.deepEqual(j(L.viewParams(catalog, 'Auto/Compras', '')), {labelName:'Auto/Compras'});
  assert.deepEqual(j(L.viewParams(catalog, 'Auto/Compras', 'b')), {account:'b', label:'L2'});
  assert.deepEqual(j(L.viewParams(catalog, 'Trabajo/Hoy', 'a')), {labelName:'Trabajo/Hoy'});
});

test('picker: a label is set only when every selected thread has it', () => {
  const threads = [{accountId:'a', labels:['L1','L3']}, {accountId:'b', labels:['L2']}];
  const s = L.pickerState(catalog, threads);
  assert.equal(s.set['Auto/Compras'], true);
  assert.equal(s.set['Trabajo'], false);       // thread in b has no such label
  assert.equal(s.set['Trabajo/Hoy'], false);
  assert.equal(L.pickerState(catalog, []).set['Trabajo'], false);
});

test('picker rows: filter, create row first when nothing matches, last when partial', () => {
  const merged = L.mergeByName(catalog);
  assert.deepEqual(L.pickerRows(merged, '').map(r => r.name), merged.map(m => m.name));
  assert.deepEqual(L.pickerRows(merged, 'compr').map(r => [r.kind, r.name]), [['label','Auto/Compras'],['create','compr']]);
  assert.deepEqual(L.pickerRows(merged, 'zzz').map(r => [r.kind, r.name]), [['create','zzz']]);
  assert.deepEqual(L.pickerRows(merged, 'trabajo').map(r => r.kind), ['label','label']); // exact (case-insens.) match: no create
  assert.deepEqual(L.pickerRows(merged, '  '), L.pickerRows(merged, '')); 
});

test('picker diff yields only real changes', () => {
  const initial = {A:true, B:false, C:false};
  assert.deepEqual(j(L.pickerChanges(initial, {A:false, B:true, C:false})), {add:['B'], remove:['A']});
  assert.deepEqual(j(L.pickerChanges(initial, {A:true, B:false})), {add:[], remove:[]});
  assert.deepEqual(j(L.pickerChanges(initial, {New:true})), {add:['New'], remove:[]});
});

test('create-before-apply targets only label-capable accounts that lack the name', () => {
  const accounts = [{id:'a',type:'gmail'},{id:'b',type:'gmail'},{id:'c',type:'imap'}];
  const threads = [{accountId:'a'},{accountId:'b'},{accountId:'b'},{accountId:'c'}];
  assert.deepEqual(L.accountsNeedingLabel(catalog, accounts, threads, 'Trabajo'), ['b']);
  assert.deepEqual(L.accountsNeedingLabel(catalog, accounts, threads, 'Brand/New'), ['a','b']);
  assert.deepEqual(L.accountsNeedingLabel(catalog, accounts, threads, 'Auto/Compras'), []);
});

test('settings helpers: per-account list, children, monitored lists', () => {
  assert.deepEqual(L.accountLabels(catalog, 'a').map(l => l.id), ['L1','L5','L3']);
  assert.equal(L.childCount(catalog, 'b', 'Trabajo'), 1);
  assert.equal(L.childCount(catalog, 'a', 'Trabajo'), 0);
  assert.deepEqual(L.monitoredAfter(catalog, 'b', 'L2', true).sort(), ['L2','L4']);
  assert.deepEqual(L.monitoredAfter(catalog, 'b', 'L4', false), []);
  const next = L.withMonitored(catalog, 'a', 'L3', true);
  assert.equal(next.find(l => l.id === 'L3').monitored, true);
  assert.equal(catalog.find(l => l.id === 'L3').monitored, false);
  assert.equal(L.samePair({background:'#AABBCC', text:'#fff'}, {background:'#aabbcc', text:'#FFF'}), true);
  assert.equal(L.nameExists(catalog, 'Trabajo'), true);
});
