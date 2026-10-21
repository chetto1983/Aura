# Craft Office save bridge checks

Date: 2026-10-22, Europe/Rome. Installed browser apps on Aura GHCR revision
`9ef3a2e73128a4dba6016c719a7bcc931e04452d`. This is capability/contract evidence for Task 4,
not a claim that the new Documents UI is implemented.

Two generated synthetic fixtures were opened in fresh host-mode iframes. The probe made a
small edit and used the app's Save shortcut. It captured bridge messages without sending any
save request to Aura's object store; no source object or original fixture was overwritten.

| Input | Save message | Bytes |
|---|---|---:|
| `aura-format-probe.doc` | `wordcraft:save`, name `aura-format-probe.docx` | 5964 |
| `aura-format-probe.ods` | `gridcraft:save`, name `aura-format-probe (imported).xlsx` | 4151 |

Both apps sent this order: ready, dirty=false, dirty=true after editing, save with converted
filename/bytes, then dirty=false. The last message arrived even though the probe never
acknowledged persistence. The native dirty flag therefore describes the app's local save state;
it cannot establish that Aura has stored the bytes.

The existing `saveBeside` path already writes a different returned filename beside the original.
The rebuilt document view must preserve that behavior and identify the saved filename, so a
converted copy is not presented as an overwritten original.

`CraftFrame` currently clears its close guard from native dirty=false while its asynchronous
host save may still be pending or may fail. Task 4 must keep pending/failed host writes in the
unsaved guard, offer a real retry, and preserve save order for repeated saves. Test save followed
immediately by dirty=false while the host request is unresolved or fails; a successful native
message alone must not permit the draft to be discarded. Metadata saves and file-content saves
also need distinct labels in the document workspace.
