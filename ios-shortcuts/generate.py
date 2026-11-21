#!/usr/bin/env python3
"""Generate the iOS Shortcut templates (Send to Copia.plist, Get from Copia.plist).

The templates contain only placeholders (__SERVER__, __TOKEN__, __DEVICE__);
build.sh fills them in and signs the result on a Mac. Standard library only.

    python3 ios-shortcuts/generate.py
"""
import os
import plistlib

HERE = os.path.dirname(os.path.abspath(__file__))


def out(uuid, name):
    return {"OutputName": name, "OutputUUID": uuid, "Type": "ActionOutput"}


def attachment(uuid, name):
    return {"Value": out(uuid, name), "WFSerializationType": "WFTextTokenAttachment"}


def text(s):
    return {"Value": {"string": s, "attachmentsByRange": {}}, "WFSerializationType": "WFTextTokenString"}


def text_var(uuid, name):
    return {"Value": {"string": "￼", "attachmentsByRange": {"{0, 1}": out(uuid, name)}},
            "WFSerializationType": "WFTextTokenString"}


def dict_field(items):
    return {"Value": {"WFDictionaryFieldValueItems": items}, "WFSerializationType": "WFDictionaryFieldValue"}


def text_item(key, value):
    return {"WFItemType": 0, "WFKey": text(key), "WFValue": value}


def file_item(key, value):
    return {"WFItemType": 5, "WFKey": text(key), "WFValue": value}


HEADERS = dict_field([
    text_item("Authorization", text("Bearer __TOKEN__")),
    text_item("X-Copia-Device", text("__DEVICE__")),
])


def action(ident, **params):
    return {"WFWorkflowActionIdentifier": ident, "WFWorkflowActionParameters": params}


def request(uuid, method, path, **extra):
    return action("is.workflow.actions.downloadurl", UUID=uuid, WFURL=f"__SERVER__{path}",
                  WFHTTPMethod=method, ShowHeaders=True, WFHTTPHeaders=HEADERS, **extra)


def post_form(uuid, form_items):
    return request(uuid, "POST", "/clip", WFHTTPBodyType="Form", WFFormValues=dict_field(form_items))


def if_is(group, input_uuid, input_name, value):
    return action("is.workflow.actions.conditional", GroupingIdentifier=group, WFControlFlowMode=0,
                  WFCondition=4, WFConditionalActionString=value,
                  WFInput={"Type": "Variable", "Variable": attachment(input_uuid, input_name)})


def otherwise(group):
    return action("is.workflow.actions.conditional", GroupingIdentifier=group, WFControlFlowMode=1)


def end_if(group, uuid):
    return action("is.workflow.actions.conditional", GroupingIdentifier=group, WFControlFlowMode=2, UUID=uuid)


def copy_to_clipboard(uuid, name):
    return action("is.workflow.actions.setclipboard", WFInput=attachment(uuid, name))


def notification(body):
    return action("is.workflow.actions.notification", WFNotificationActionBody=body, WFNotificationActionSound=False)


def workflow(actions, color, glyph):
    return {
        "WFWorkflowActions": actions,
        "WFWorkflowClientVersion": "2302.0.4",
        "WFWorkflowMinimumClientVersion": 900,
        "WFWorkflowMinimumClientVersionString": "900",
        "WFWorkflowIcon": {"WFWorkflowIconStartColor": color, "WFWorkflowIconGlyphNumber": glyph},
        "WFWorkflowImportQuestions": [],
        "WFWorkflowInputContentItemClasses": [],
        "WFWorkflowOutputContentItemClasses": [],
        "WFWorkflowTypes": [],
        "WFWorkflowHasOutputFallback": False,
        "WFWorkflowHasShortcutInputVariables": False,
        "WFQuickActionSurfaces": [],
    }


def send_to_copia():
    clip, kind, group = "C0F1A001-0000-4000-8000-000000000001", "C0F1A001-0000-4000-8000-000000000002", "C0F1A001-0000-4000-8000-0000000000A0"
    return workflow([
        action("is.workflow.actions.getclipboard", UUID=clip),
        action("is.workflow.actions.getitemtype", UUID=kind, WFInput=attachment(clip, "Clipboard")),
        if_is(group, kind, "Type", "Text"),
        post_form("C0F1A001-0000-4000-8000-000000000004", [text_item("text", text_var(clip, "Clipboard"))]),
        otherwise(group),
        post_form("C0F1A001-0000-4000-8000-000000000006", [file_item("file", attachment(clip, "Clipboard"))]),
        end_if(group, "C0F1A001-0000-4000-8000-000000000007"),
        notification("Sent to Copia"),
    ], color=4282601983, glyph=59511)


def get_from_copia():
    # Ask the server what the item is first: a plain-text download can arrive in
    # Shortcuts as a *file*, which pastes as nothing in a text field, so text is
    # converted with "Get Text from Input" before it's copied.
    meta, kind, group = "C0F1A002-0000-4000-8000-000000000001", "C0F1A002-0000-4000-8000-000000000002", "C0F1A002-0000-4000-8000-0000000000A0"
    raw_text, as_text, raw_other = ("C0F1A002-0000-4000-8000-000000000003", "C0F1A002-0000-4000-8000-000000000004",
                                    "C0F1A002-0000-4000-8000-000000000005")
    return workflow([
        request(meta, "GET", "/clip/latest"),
        action("is.workflow.actions.getvalueforkey", UUID=kind, WFDictionaryKey="kind",
               WFGetDictionaryValueType="Value", WFInput=attachment(meta, "Contents of URL")),
        if_is(group, kind, "Dictionary Value", "text"),
        request(raw_text, "GET", "/clip/latest/raw"),
        action("is.workflow.actions.detect.text", UUID=as_text, WFInput=attachment(raw_text, "Contents of URL")),
        copy_to_clipboard(as_text, "Text"),
        otherwise(group),
        request(raw_other, "GET", "/clip/latest/raw"),
        copy_to_clipboard(raw_other, "Contents of URL"),
        end_if(group, "C0F1A002-0000-4000-8000-000000000006"),
        notification("Copied from Copia"),
    ], color=4292093695, glyph=59446)


if __name__ == "__main__":
    for name, wf in [("Send to Copia", send_to_copia()), ("Get from Copia", get_from_copia())]:
        path = os.path.join(HERE, f"{name}.plist")
        with open(path, "wb") as f:
            plistlib.dump(wf, f, fmt=plistlib.FMT_XML, sort_keys=True)
        print("wrote", os.path.relpath(path))
