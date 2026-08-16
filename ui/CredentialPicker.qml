import QtQuick
import modules 1.0 as System
import "."

System.Picker {
    id: picker

    property bool customMode: false
    property bool selectionPending: false
    property var customItem: null

    open: Backend.open
    placeholder: customMode ? "Exact 1Password field label…" : "Type into " + Backend.target + " — item, email, password, or OTP…"
    items: customMode ? [] : Backend.suggestions
    subtitleField: "subtitle"
    filterItemsWithQuery: false
    freeText: customMode
    listVisible: !customMode
    loading: Backend.status === "loading"
    refreshing: Backend.status === "refreshing"
    emptyText: customMode ? "type a field label and press Enter" : (Backend.message || "no matching credentials")
    enterLabel: customMode ? "type field" : "type"
    altKey: Qt.Key_O
    altLabel: customMode ? "" : "Ctrl+O: custom field · Ctrl+R: refresh"
    enterKeepsOpen: true

    onQueryChanged: {
        if (!customMode)
            Backend.search(query)
    }

    onEnter: item => {
        if (!item)
            return
        selectionPending = true
        Backend.select(item, item.field_kind, item.field_label)
    }

    onEnterText: text => {
        if (!customItem || !text.trim())
            return
        selectionPending = true
        Backend.select(customItem, "custom", text.trim())
    }

    onAltAction: item => {
        if (!item)
            return
        customItem = item
        customMode = true
        Backend.message = "Only this exact field will be fetched"
    }

    onCtrlR: item => Backend.refresh()

    onCloseRequested: {
        if (!selectionPending)
            Backend.cancel()
    }

    Connections {
        target: Backend
        function onResetRequested() {
            picker.customMode = false
            picker.selectionPending = false
            picker.customItem = null
        }
    }
}
