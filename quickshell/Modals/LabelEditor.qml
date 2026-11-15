import QtQuick
import QtQuick.Layouts
import qs.Common
import qs.Services
import qs.Widgets
import "../Common/Labels.js" as Labels

// Inline create/rename/recolor form for one label of one account. Colors come
// only from the account's labels.palette; without a palette the colour row is
// hidden (the provider then takes no colour choice from us).
ColumnLayout {
    id: editor

    // labelId "" creates a new label.
    property string accountId: ""
    property string labelId: ""
    property string initialName: ""
    property string initialBg: ""
    property string initialText: ""
    property int childCount: 0

    property string name: initialName
    property string bg: initialBg
    property string fg: initialText
    property bool busy: false
    property string error: ""

    readonly property var colorPairs: DankMailService.labelPalettes[accountId] || []
    readonly property bool isNew: labelId === ""
    readonly property string trimmed: name.trim()
    readonly property bool colorChanged: bg !== "" && (bg.toLowerCase() !== initialBg.toLowerCase() || fg.toLowerCase() !== initialText.toLowerCase())
    // Picking the "no color" swatch on a label that has a color clears it.
    readonly property bool colorCleared: !isNew && bg === "" && initialBg !== ""
    readonly property bool canSave: !busy && trimmed !== "" && (isNew || trimmed !== initialName || colorChanged || colorCleared)

    signal done

    spacing: Theme.spacingS

    // The form lives hidden next to its row; every time it is shown it starts
    // from the label's current values.
    function reset() {
        name = initialName;
        bg = initialBg;
        fg = initialText;
        error = "";
        busy = false;
        nameField.text = initialName;
        DankMailService.loadLabelPalette(accountId, null);
        nameField.forceActiveFocus();
    }

    onVisibleChanged: if (visible) reset()

    function save() {
        if (!canSave)
            return;
        busy = true;
        error = "";
        const finish = resp => {
            busy = false;
            if (resp.error) {
                error = resp.error;
                return;
            }
            done();
        };
        if (isNew) {
            DankMailService.createLabel(accountId, trimmed, bg, fg, finish);
        } else {
            const fields = {};
            if (trimmed !== initialName)
                fields.name = trimmed;
            if (colorChanged) {
                fields.colorBg = bg;
                fields.colorText = fg;
            } else if (colorCleared) {
                // Both present and empty: the daemon removes the color.
                fields.colorBg = "";
                fields.colorText = "";
            }
            DankMailService.updateLabel(accountId, labelId, fields, finish);
        }
    }

    DankTextField {
        id: nameField
        Layout.fillWidth: true
        Layout.preferredHeight: 40
        placeholderText: I18n.tr("Label name (use / to nest)", "label settings")
        onTextChanged: editor.name = text
        onAccepted: editor.save()
        onKeyPressed: event => {
            if (event.key === Qt.Key_Escape) {
                editor.done();
                event.accepted = true;
            }
        }
    }

    StyledText {
        Layout.fillWidth: true
        visible: !editor.isNew && editor.childCount > 0
        text: I18n.tr("Renaming does not rename the labels nested under it.", "label settings")
        font.pixelSize: Theme.fontSizeSmall
        color: Theme.surfaceTextMedium
        wrapMode: Text.WordWrap
    }

    Flow {
        Layout.fillWidth: true
        visible: editor.colorPairs.length > 0
        spacing: Theme.spacingXS

        Rectangle {
            id: noColorSwatch
            readonly property bool chosen: editor.bg === ""

            width: 26
            height: 26
            radius: 13
            color: "transparent"
            border.width: chosen ? 2 : 1
            border.color: chosen ? Theme.primary : Theme.outlineMedium

            DankIcon {
                anchors.centerIn: parent
                name: "format_color_reset"
                size: 14
                color: Theme.surfaceTextMedium
            }

            StateLayer {
                stateColor: Theme.primary
                cornerRadius: 13
                onClicked: {
                    editor.bg = "";
                    editor.fg = "";
                }
            }
        }

        Repeater {
            model: editor.colorPairs

            delegate: Rectangle {
                id: swatch
                required property var modelData
                readonly property bool chosen: Labels.samePair(modelData, {background: editor.bg, text: editor.fg})

                width: 26
                height: 26
                radius: 13
                color: modelData.background
                border.width: chosen ? 2 : 1
                border.color: chosen ? Theme.primary : Theme.outlineMedium

                DankIcon {
                    anchors.centerIn: parent
                    visible: swatch.chosen
                    name: "check"
                    size: 14
                    color: swatch.modelData.text
                }

                StateLayer {
                    stateColor: Theme.primary
                    cornerRadius: 13
                    onClicked: {
                        editor.bg = swatch.modelData.background;
                        editor.fg = swatch.modelData.text;
                    }
                }
            }
        }
    }

    StyledText {
        Layout.fillWidth: true
        visible: editor.error !== ""
        text: editor.error
        textFormat: Text.PlainText
        wrapMode: Text.Wrap
        font.pixelSize: Theme.fontSizeSmall
        color: Theme.error
    }

    RowLayout {
        Layout.fillWidth: true
        spacing: Theme.spacingS

        Item {
            Layout.fillWidth: true
        }

        DankButton {
            buttonHeight: 32
            horizontalPadding: Theme.spacingM
            text: I18n.tr("Cancel", "label settings")
            backgroundColor: Theme.surfaceContainerHighest
            textColor: Theme.surfaceText
            enabled: !editor.busy
            onClicked: editor.done()
        }

        DankButton {
            buttonHeight: 32
            horizontalPadding: Theme.spacingM
            text: editor.isNew ? I18n.tr("Create", "label settings") : I18n.tr("Save", "label settings")
            busy: editor.busy
            enabled: editor.canSave
            onClicked: editor.save()
        }
    }
}
