import QtQuick
import QtQuick.Layouts
import qs.Common
import qs.Services
import qs.Widgets
import "../Common/Labels.js" as Labels

// Label picker overlay (opened with "l" or the Label buttons) for one or
// several threads. It is an overlay inside the triage window, not a window of
// its own, so it behaves in tiling compositors. A label shows as set when
// every selected thread carries it; Enter applies the diff through ops.label.
Item {
    id: picker

    visible: false
    z: 200

    property var targets: []
    property var initial: ({})
    property var desired: ({})
    property int highlight: 0
    property bool busy: false
    property string error: ""

    readonly property var merged: Labels.pickerState(DankMailService.labelCatalog, targets).merged
    readonly property var rows: Labels.pickerRows(merged, search.text)
    readonly property var changes: Labels.pickerChanges(initial, desired)
    readonly property int changeCount: changes.add.length + changes.remove.length

    signal closed

    function open(threads) {
        if (!threads || threads.length === 0 || !DankMailService.labelsAvailable)
            return;
        targets = threads;
        const st = Labels.pickerState(DankMailService.labelCatalog, threads);
        initial = Object.assign({}, st.set);
        desired = Object.assign({}, st.set);
        highlight = 0;
        busy = false;
        error = "";
        search.text = "";
        visible = true;
        search.forceActiveFocus();
    }

    function cancel() {
        if (busy)
            return;
        visible = false;
        closed();
    }

    function isSet(name) {
        return desired[name] === true;
    }

    function toggle(name) {
        const next = Object.assign({}, desired);
        next[name] = !isSet(name);
        desired = next;
    }

    // Creates `name` in every selected account that supports labels and lacks
    // it, then runs `done`.
    function createEverywhere(name, done) {
        const accounts = Labels.accountsNeedingLabel(DankMailService.labelCatalog, DankMailService.accounts, targets, name);
        let pending = accounts.length;
        let failed = "";
        if (pending === 0) {
            done("");
            return;
        }
        accounts.forEach(acc => {
            DankMailService.createLabel(acc, name, "", "", resp => {
                if (resp.error && failed === "")
                    failed = resp.error;
                pending--;
                if (pending === 0)
                    done(failed);
            });
        });
    }

    function applyChanges(add, remove) {
        if (add.length === 0 && remove.length === 0) {
            cancel();
            return;
        }
        busy = true;
        error = "";
        const ids = targets.map(t => t.id);
        const fresh = add.filter(n => !Labels.nameExists(DankMailService.labelCatalog, n));
        let left = fresh.length;
        const send = () => {
            DankMailService.opLabel(ids, add, remove, resp => {
                busy = false;
                if (resp.error) {
                    error = resp.error;
                    return;
                }
                visible = false;
                closed();
            });
        };
        if (left === 0) {
            send();
            return;
        }
        let failed = "";
        fresh.forEach(name => createEverywhere(name, err => {
            if (err && failed === "")
                failed = err;
            left--;
            if (left === 0) {
                if (failed !== "") {
                    busy = false;
                    error = I18n.tr("Could not create the label: %1", "label picker").arg(failed);
                } else {
                    send();
                }
            }
        }));
    }

    function activateRow(row) {
        if (!row)
            return;
        if (row.kind === "create") {
            const d = Object.assign({}, desired);
            d[row.name] = true;
            desired = d;
            const ch = Labels.pickerChanges(initial, d);
            applyChanges(ch.add, ch.remove);
        } else {
            toggle(row.name);
        }
    }

    function accept() {
        if (busy)
            return;
        const row = rows[highlight];
        if (row && row.kind === "create") {
            activateRow(row);
            return;
        }
        if (changeCount === 0 && row)
            toggle(row.name);
        const ch = Labels.pickerChanges(initial, desired);
        applyChanges(ch.add, ch.remove);
    }

    function moveHighlight(delta) {
        if (rows.length === 0)
            return;
        highlight = Math.max(0, Math.min(rows.length - 1, highlight + delta));
        list.positionViewAtIndex(highlight, ListView.Contain);
    }

    Rectangle {
        anchors.fill: parent
        color: Qt.rgba(0, 0, 0, 0.45)

        MouseArea {
            anchors.fill: parent
            onClicked: picker.cancel()
        }
    }

    StyledRect {
        id: card
        anchors.centerIn: parent
        width: Math.min(400, parent.width - Theme.spacingXL * 2)
        height: Math.min(parent.height - Theme.spacingXL * 2, cardColumn.implicitHeight + Theme.spacingL * 2)
        radius: Theme.cornerRadiusLarge
        color: Theme.surfaceContainerHigh
        border.width: 1
        border.color: Theme.outlineLight

        // Swallow clicks so the scrim underneath does not cancel.
        MouseArea {
            anchors.fill: parent
        }

        ColumnLayout {
            id: cardColumn
            anchors.fill: parent
            anchors.margins: Theme.spacingL
            spacing: Theme.spacingM

            RowLayout {
                Layout.fillWidth: true
                spacing: Theme.spacingS

                DankIcon {
                    name: "label"
                    size: Theme.iconSize
                    color: Theme.primary
                }

                StyledText {
                    Layout.fillWidth: true
                    text: I18n.tr("Labels", "label picker")
                    font.pixelSize: Theme.fontSizeLarge
                    font.weight: Font.DemiBold
                }

                StyledText {
                    text: picker.targets.length === 1 ? I18n.tr("1 thread", "label picker") : I18n.tr("%1 threads", "label picker").arg(picker.targets.length)
                    font.pixelSize: Theme.fontSizeSmall
                    color: Theme.surfaceTextMedium
                }
            }

            DankTextField {
                id: search
                Layout.fillWidth: true
                Layout.preferredHeight: 40
                iconName: "search"
                placeholderText: I18n.tr("Search or create a label…", "label picker")
                onTextChanged: picker.highlight = 0
                onAccepted: picker.accept()
                onKeyPressed: event => {
                    if (event.key === Qt.Key_Down) {
                        picker.moveHighlight(1);
                        event.accepted = true;
                    } else if (event.key === Qt.Key_Up) {
                        picker.moveHighlight(-1);
                        event.accepted = true;
                    } else if (event.key === Qt.Key_Tab || (event.key === Qt.Key_Space && (search.text === "" || (event.modifiers & Qt.ControlModifier)))) {
                        const row = picker.rows[picker.highlight];
                        if (row && row.kind === "label")
                            picker.toggle(row.name);
                        event.accepted = true;
                    } else if (event.key === Qt.Key_Escape) {
                        picker.cancel();
                        event.accepted = true;
                    }
                }
            }

            ListView {
                id: list
                Layout.fillWidth: true
                Layout.preferredHeight: Math.min(280, Math.max(48, contentHeight))
                Layout.fillHeight: true
                clip: true
                model: picker.rows
                boundsBehavior: Flickable.StopAtBounds
                spacing: 2

                delegate: StyledRect {
                    id: pickRow
                    required property var modelData
                    required property int index
                    readonly property bool isCreate: modelData.kind === "create"
                    readonly property bool checked: !isCreate && picker.isSet(modelData.name)
                    readonly property bool current: index === picker.highlight

                    width: list.width
                    height: 36
                    radius: Theme.cornerRadiusSmall
                    color: current ? Theme.withAlpha(Theme.primary, 0.16) : "transparent"

                    RowLayout {
                        anchors.fill: parent
                        anchors.leftMargin: Theme.spacingM
                        anchors.rightMargin: Theme.spacingM
                        spacing: Theme.spacingM

                        Rectangle {
                            visible: !pickRow.isCreate
                            width: 18
                            height: 18
                            radius: 4
                            color: pickRow.checked ? Theme.primary : "transparent"
                            border.width: 2
                            border.color: pickRow.checked ? Theme.primary : Theme.outlineMedium

                            DankIcon {
                                anchors.centerIn: parent
                                visible: pickRow.checked
                                name: "check"
                                size: 13
                                color: Theme.primaryText
                            }
                        }

                        DankIcon {
                            visible: pickRow.isCreate
                            name: "add"
                            size: 18
                            color: Theme.primary
                        }

                        Rectangle {
                            visible: !pickRow.isCreate
                            width: 10
                            height: 10
                            radius: 5
                            color: (!pickRow.isCreate && pickRow.modelData.entry.colorBg !== "") ? pickRow.modelData.entry.colorBg : Theme.outlineMedium
                        }

                        StyledText {
                            Layout.fillWidth: true
                            text: pickRow.isCreate ? I18n.tr("Create label “%1”", "label picker").arg(pickRow.modelData.name) : pickRow.modelData.name
                            textFormat: Text.PlainText
                            elide: Text.ElideRight
                            maximumLineCount: 1
                            font.pixelSize: Theme.fontSizeMedium
                            color: pickRow.isCreate ? Theme.primary : Theme.surfaceText
                        }
                    }

                    StateLayer {
                        stateColor: Theme.primary
                        onClicked: {
                            picker.highlight = pickRow.index;
                            picker.activateRow(pickRow.modelData);
                            search.forceActiveFocus();
                        }
                    }
                }

                StyledText {
                    anchors.centerIn: parent
                    visible: picker.rows.length === 0
                    text: I18n.tr("No labels yet", "label picker")
                    color: Theme.surfaceTextMedium
                }
            }

            StyledText {
                Layout.fillWidth: true
                visible: picker.error !== ""
                text: picker.error
                textFormat: Text.PlainText
                wrapMode: Text.Wrap
                color: Theme.error
                font.pixelSize: Theme.fontSizeSmall
            }

            StyledText {
                Layout.fillWidth: true
                text: I18n.tr("↑↓ move · Tab, Space or click toggles · Enter applies · Esc cancels", "label picker")
                wrapMode: Text.Wrap
                color: Theme.surfaceTextMedium
                font.pixelSize: Theme.fontSizeSmall
            }

            RowLayout {
                Layout.fillWidth: true
                spacing: Theme.spacingS

                Item {
                    Layout.fillWidth: true
                }

                DankButton {
                    buttonHeight: 34
                    horizontalPadding: Theme.spacingM
                    text: I18n.tr("Cancel", "label picker")
                    backgroundColor: Theme.surfaceContainerHighest
                    textColor: Theme.surfaceText
                    enabled: !picker.busy
                    onClicked: picker.cancel()
                }

                DankButton {
                    buttonHeight: 34
                    horizontalPadding: Theme.spacingM
                    text: I18n.tr("Apply", "label picker")
                    busy: picker.busy
                    enabled: !picker.busy && picker.changeCount > 0
                    onClicked: picker.accept()
                }
            }
        }
    }
}
