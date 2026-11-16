const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const source = fs.readFileSync(path.join(__dirname, '../../quickshell/Common/Selection.js'), 'utf8');
const api = vm.runInNewContext(source.replace(/^\.pragma library\s*/, '') + '\n({selectableIds, allSelected, toggleAll})');
const plain = value => JSON.parse(JSON.stringify(value));
const rows = ids => ids.map(id => ({id, subject: `Thread ${id}`}));

test('selectable ids follow list order and skip rows hidden by a pending action', () => {
    assert.deepEqual(plain(api.selectableIds(rows([5, 3, 9, 1]), [3])), [5, 9, 1]);
    assert.deepEqual(plain(api.selectableIds(rows([5, 3]), [])), [5, 3]);
    assert.deepEqual(plain(api.selectableIds([], [1])), []);
    assert.deepEqual(plain(api.selectableIds(null, null)), []);
});

test('allSelected is true only when every listed row is checked', () => {
    assert.equal(api.allSelected([1, 2, 3], [1, 2, 3]), true);
    assert.equal(api.allSelected([3, 1, 2, 99], [1, 2, 3]), true);
    assert.equal(api.allSelected([1, 2], [1, 2, 3]), false);
    assert.equal(api.allSelected([], []), false);
    assert.equal(api.allSelected([7], []), false);
});

test('toggleAll selects every listed row, keeping the ones already checked first', () => {
    assert.deepEqual(plain(api.toggleAll([3], [1, 2, 3, 4])), [3, 1, 2, 4]);
    assert.deepEqual(plain(api.toggleAll([], [1, 2])), [1, 2]);
});

test('toggleAll on a fully selected list clears the listed rows only', () => {
    assert.deepEqual(plain(api.toggleAll([1, 2, 3], [1, 2, 3])), []);
    // A checked id that is no longer listed (hidden by an undoable action)
    // is not this button's to drop.
    assert.deepEqual(plain(api.toggleAll([9, 1, 2], [1, 2])), [9]);
});

test('toggleAll never mutates its inputs and tolerates an empty list', () => {
    const checked = [2], listed = [1, 2];
    api.toggleAll(checked, listed);
    assert.deepEqual(checked, [2]);
    assert.deepEqual(listed, [1, 2]);
    assert.deepEqual(plain(api.toggleAll([4], [])), [4]);
});
