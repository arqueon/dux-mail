.pragma library

// Pure logic for the triage list's bulk selection ("select all"). No QML
// types here so node can test it.

// Ids a "select all" covers: every loaded row of the current list, in list
// order, except rows hidden while an undoable action is pending.
function selectableIds(threads, hiddenIds) {
    var hidden = hiddenIds || [], out = [];
    for (var i = 0; i < (threads || []).length; i++) {
        if (hidden.indexOf(threads[i].id) === -1)
            out.push(threads[i].id);
    }
    return out;
}

function allSelected(checkedIds, selectable) {
    if (!selectable || selectable.length === 0)
        return false;
    for (var i = 0; i < selectable.length; i++) {
        if (checkedIds.indexOf(selectable[i]) === -1)
            return false;
    }
    return true;
}

// The next checked list after pressing "select all": everything listed, or,
// when everything listed is already checked, the checked ids minus the
// listed ones. Never mutates its inputs.
function toggleAll(checkedIds, selectable) {
    var next = checkedIds.slice(), i;
    if (allSelected(checkedIds, selectable)) {
        return next.filter(function (id) { return selectable.indexOf(id) === -1; });
    }
    for (i = 0; i < selectable.length; i++) {
        if (next.indexOf(selectable[i]) === -1)
            next.push(selectable[i]);
    }
    return next;
}
