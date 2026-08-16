import QtQuick
import modules 1.0 as System
import "."

System.Picker {
    id: picker

    property bool fieldMode: false
    property bool customMode: false
    property bool selectionPending: false
    property var selectedItem: null
    property var fieldItems: []

    function fieldsFor(item) {
        const out = []
        const fields = item.fields || []
        for (let i = 0; i < fields.length; i++) {
            const field = fields[i]
            let label = field.label
            if (field.kind === "username")
                label = "Username / email"
            else if (field.kind === "otp")
                label = "One-time password"
            else if (field.kind === "password")
                label = "Password"
            out.push({
                item_id: item.item_id,
                field_kind: field.kind,
                field_label: field.label,
                label: label,
                subtitle: item.title
            })
        }
        return out
    }

    function showItems() {
        fieldMode = false
        customMode = false
        selectedItem = null
        fieldItems = []
        clearQuery()
    }

    open: Backend.open
    placeholder: customMode ? "Exact 1Password field label…" : (fieldMode ? "Choose a field…" : "Search 1Password…")
    items: customMode ? [] : (fieldMode ? fieldItems : Backend.suggestions)
    subtitleField: "subtitle"
    filterItemsWithQuery: false
    freeText: customMode
    listVisible: !customMode
    loading: Backend.status === "loading"
    refreshing: Backend.status === "refreshing"
    emptyText: customMode ? "type a field label and press Enter" : (Backend.message || (fieldMode ? "no standard fields" : "no matching credentials"))
    enterLabel: customMode ? "type field" : (fieldMode ? "type" : "choose")
    altKey: Qt.Key_O
    altLabel: customMode ? "Esc: fields" : (fieldMode ? "Esc: items · Ctrl+O: custom field" : "Ctrl+O: custom field · Ctrl+R: refresh")
    enterKeepsOpen: true

    onQueryChanged: {
        if (!customMode && !fieldMode)
            Backend.search(query)
    }

    onEnter: item => {
        if (!item)
            return
        if (!fieldMode) {
            selectedItem = item
            fieldItems = fieldsFor(item)
            fieldMode = true
            clearQuery()
            return
        }
        selectionPending = true
        Backend.select(item, item.field_kind, item.field_label)
    }

    onEnterText: text => {
        if (!selectedItem || !text.trim())
            return
        selectionPending = true
        Backend.select(selectedItem, "custom", text.trim())
    }

    onAltAction: item => {
        const target = fieldMode ? selectedItem : item
        if (!target)
            return
        selectedItem = target
        fieldMode = true
        customMode = true
        clearQuery()
        Backend.message = "Only this exact field will be fetched"
    }

    onCtrlR: item => Backend.refresh()

    onCloseRequested: {
        if (selectionPending)
            return
        if (customMode) {
            customMode = false
            Backend.message = ""
            clearQuery()
        } else if (fieldMode) {
            showItems()
        } else {
            Backend.cancel()
        }
    }

    Connections {
        target: Backend
        function onResetRequested() {
            picker.fieldMode = false
            picker.customMode = false
            picker.selectionPending = false
            picker.selectedItem = null
            picker.fieldItems = []
        }
    }
}
