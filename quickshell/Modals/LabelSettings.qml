import QtQuick
import QtQuick.Layouts
import qs.Common
import qs.Services
import qs.Widgets
import "../Common/Labels.js" as Labels

// "Manage labels" section of Settings: per label-capable account, create,
// rename, recolor, delete (with an explicit irreversible-confirmation) and the
// "Always sync" toggle. Hidden when no account supports labels.
ColumnLayout {
    id: section

    // Row state lives here (not in delegates) so it survives the catalog
    // refresh that follows every change.
    property string openAccount: ""
    property string editingKey: ""
    property string confirmKey: ""
    property string deleteError: ""

    function keyOf(accountId, labelId) {
        return accountId + "|" + labelId;
    }

    visible: DankMailService.labelsAvailable
    spacing: Theme.spacingM

    StyledText {
        text: I18n.tr("Manage labels", "settings section")
        font.pixelSize: Theme.fontSizeMedium
        font.weight: Font.DemiBold
        color: Theme.primary
    }

    StyledText {
        Layout.fillWidth: true
        text: I18n.tr("Labels belong to each account. Labels marked “Always sync” stay in sync with the provider on every sync.", "settings")
        font.pixelSize: Theme.fontSizeSmall
        color: Theme.surfaceTextMedium
        wrapMode: Text.WordWrap
    }

    Repeater {
        model: DankMailService.labelAccounts

        delegate: StyledRect {
            id: acct
            required property var modelData
            readonly property bool open: section.openAccount === modelData.id
            readonly property var labels: Labels.accountLabels(DankMailService.labelCatalog, modelData.id)

            Layout.fillWidth: true
            implicitHeight: acctColumn.implicitHeight + Theme.spacingM * 2
            color: Theme.surfaceContainer

            ColumnLayout {
                id: acctColumn
                anchors.fill: parent
                anchors.margins: Theme.spacingM
                spacing: Theme.spacingS

                // Account header: click to expand.
                Item {
                    Layout.fillWidth: true
                    Layout.preferredHeight: 32

                    RowLayout {
                        anchors.fill: parent
                        spacing: Theme.spacingM

                        Rectangle {
                            width: 10
                            height: 10
                            radius: 5
                            color: DankMailService.accountColor(acct.modelData.id)
                        }

                        StyledText {
                            Layout.fillWidth: true
                            text: acct.modelData.email
                            elide: Text.ElideMiddle
                            font.pixelSize: Theme.fontSizeSmall
                            font.weight: Font.DemiBold
                        }

                        StyledText {
                            text: acct.labels.length === 1 ? I18n.tr("1 label", "label settings") : I18n.tr("%1 labels", "label settings").arg(acct.labels.length)
                            font.pixelSize: Theme.fontSizeSmall
                            color: Theme.surfaceTextMedium
                        }

                        DankIcon {
                            name: acct.open ? "expand_less" : "expand_more"
                            size: Theme.iconSize
                            color: Theme.surfaceTextMedium
                        }
                    }

                    StateLayer {
                        stateColor: Theme.primary
                        onClicked: {
                            section.openAccount = acct.open ? "" : acct.modelData.id;
                            section.editingKey = "";
                            section.confirmKey = "";
                        }
                    }
                }

                // New label.
                DankButton {
                    visible: acct.open && section.editingKey !== section.keyOf(acct.modelData.id, "")
                    buttonHeight: 32
                    horizontalPadding: Theme.spacingM
                    text: I18n.tr("New label", "label settings")
                    iconName: "add"
                    backgroundColor: Theme.primaryContainer
                    textColor: Theme.onPrimaryContainer
                    onClicked: {
                        section.confirmKey = "";
                        section.editingKey = section.keyOf(acct.modelData.id, "");
                    }
                }

                LabelEditor {
                    Layout.fillWidth: true
                    visible: acct.open && section.editingKey === section.keyOf(acct.modelData.id, "")
                    accountId: acct.modelData.id
                    onDone: section.editingKey = ""
                }

                Repeater {
                    model: acct.open ? acct.labels : []

                    delegate: ColumnLayout {
                        id: labelRow
                        required property var modelData
                        readonly property string rowKey: section.keyOf(modelData.accountId, modelData.id)
                        readonly property bool editing: section.editingKey === rowKey
                        readonly property bool confirming: section.confirmKey === rowKey

                        Layout.fillWidth: true
                        spacing: Theme.spacingS

                        Rectangle {
                            Layout.fillWidth: true
                            Layout.preferredHeight: 1
                            color: Theme.outlineLight
                        }

                        RowLayout {
                            Layout.fillWidth: true
                            visible: !labelRow.editing && !labelRow.confirming
                            spacing: Theme.spacingS

                            Rectangle {
                                width: 12
                                height: 12
                                radius: 6
                                color: labelRow.modelData.colorBg !== "" ? labelRow.modelData.colorBg : Theme.outlineMedium
                            }

                            StyledText {
                                Layout.fillWidth: true
                                text: labelRow.modelData.name
                                textFormat: Text.PlainText
                                elide: Text.ElideRight
                                font.pixelSize: Theme.fontSizeSmall
                            }

                            StyledText {
                                text: I18n.tr("Always sync", "label settings")
                                font.pixelSize: Theme.fontSizeSmall
                                color: Theme.surfaceTextMedium
                            }

                            DankToggle {
                                checked: labelRow.modelData.monitored
                                onToggled: checked => DankMailService.setLabelMonitored(labelRow.modelData.accountId, labelRow.modelData.id, checked, null)
                            }

                            DankActionButton {
                                buttonSize: 28
                                iconName: "edit"
                                onClicked: {
                                    section.confirmKey = "";
                                    section.editingKey = labelRow.rowKey;
                                }
                            }

                            DankActionButton {
                                buttonSize: 28
                                iconName: "delete"
                                iconColor: Theme.error
                                onClicked: {
                                    section.editingKey = "";
                                    section.deleteError = "";
                                    section.confirmKey = labelRow.rowKey;
                                }
                            }
                        }

                        LabelEditor {
                            Layout.fillWidth: true
                            visible: labelRow.editing
                            accountId: labelRow.modelData.accountId
                            labelId: labelRow.modelData.id
                            initialName: labelRow.modelData.name
                            initialBg: labelRow.modelData.colorBg
                            initialText: labelRow.modelData.colorText
                            childCount: Labels.childCount(DankMailService.labelCatalog, labelRow.modelData.accountId, labelRow.modelData.name)
                            onDone: section.editingKey = ""
                        }

                        // Irreversible: spell out what happens before deleting.
                        StyledRect {
                            id: confirmBox
                            property bool busy: false
                            Layout.fillWidth: true
                            visible: labelRow.confirming
                            implicitHeight: confirmColumn.implicitHeight + Theme.spacingM * 2
                            color: Theme.withAlpha(Theme.error, 0.12)

                            ColumnLayout {
                                id: confirmColumn
                                anchors.fill: parent
                                anchors.margins: Theme.spacingM
                                spacing: Theme.spacingS

                                StyledText {
                                    Layout.fillWidth: true
                                    text: I18n.tr("Delete the label “%1”?", "label settings").arg(labelRow.modelData.name)
                                    textFormat: Text.PlainText
                                    wrapMode: Text.Wrap
                                    font.pixelSize: Theme.fontSizeSmall
                                    font.weight: Font.DemiBold
                                }

                                StyledText {
                                    Layout.fillWidth: true
                                    text: I18n.tr("The label is removed from every thread that has it. Your mail is NOT deleted. This cannot be undone.", "label settings")
                                    wrapMode: Text.Wrap
                                    font.pixelSize: Theme.fontSizeSmall
                                }

                                StyledText {
                                    Layout.fillWidth: true
                                    visible: section.deleteError !== ""
                                    text: section.deleteError
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
                                        enabled: !confirmBox.busy
                                        onClicked: section.confirmKey = ""
                                    }

                                    DankButton {
                                        buttonHeight: 32
                                        horizontalPadding: Theme.spacingM
                                        text: I18n.tr("Delete label", "label settings")
                                        iconName: "delete"
                                        backgroundColor: Theme.error
                                        textColor: Theme.errorText
                                        busy: confirmBox.busy
                                        enabled: !confirmBox.busy
                                        onClicked: {
                                            confirmBox.busy = true;
                                            DankMailService.deleteLabel(labelRow.modelData.accountId, labelRow.modelData.id, resp => {
                                                confirmBox.busy = false;
                                                if (resp.error)
                                                    section.deleteError = resp.error;
                                                else
                                                    section.confirmKey = "";
                                            });
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}
