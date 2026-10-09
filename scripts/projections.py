"""Generate localized display maps without mutating configuration registries."""
import json
from pathlib import Path
import config_display


def quote(value):
    return json.dumps(value, ensure_ascii=False)


def guarded(path, old, new):
    text = path.read_text()
    if text.count(old) != 1:
        raise ValueError(f'context adapter must match once in {path.name}: {old}')
    path.write_text(text.replace(old, new))


def apply(source, reviewed):
    labels, descriptions, actions, values, compact, risks = {}, {}, {}, {}, {}, {}
    for e in reviewed:
        if e['decision'] != 'translate':
            continue
        if e['source'] in {'terminal', 'agent', 'session'}:
            compact[e['translation']] = e['source'][0]
        if e['kind'] == 'registry' and e['file'] == 'internal/risk/builtin.go':
            risks[e['source']] = e['translation']
        elif e['kind'] == 'display':
            key = e['id'].removeprefix('settings.')
            if '.value.' in key:
                path, value = key.split('.value.', 1)
                values[path + '\x00' + value] = e['translation']
            elif key.endswith('.label'):
                labels[key.removesuffix('.label')] = e['translation']
            else:
                descriptions[key.removesuffix('.description')] = e['translation']
        elif e['kind'] == 'registry' or (e['kind'] == 'config-display' and e['context'].startswith('Scopes |')):
            actions[e['source']] = e['translation']
            if '%s' in e['source']:
                for number in range(10):
                    actions[e['source'].replace('%s', str(number))] = e['translation'].replace('%s', str(number))
    directory = Path(source) / 'internal/app'
    funcs = '''package app

import (
 "strings"
 "github.com/Gaurav-Gosain/tuios/internal/config"
 "github.com/Gaurav-Gosain/tuios/internal/risk"
)

func fnRiskText(text string) string {
 if value, ok := fnRiskDisplay[text]; ok { return value }; return text
}
func fnRiskName(name string) string {
 if _, builtin := risk.Why(name); builtin { return fnRiskText(name) }; return name
}

func fnBindingDescription(action, text string) string {
 if strings.HasPrefix(action, config.CommandActionPrefix) { return text }
 return fnActionDescription(text)
}
func fnPrefixMenuGroups(registry *config.KeybindRegistry, prefix string, state config.MenuState) []config.KeybindingGroup {
 groups := config.FnDisplayPrefixMenuGroups(registry, prefix, state)
 for i := range groups { for j := range groups[i].Bindings {
  binding := &groups[i].Bindings[j]
  binding.Description = fnBindingDescription(binding.Action, binding.Description)
 } }
 return groups
}

func fnSettingDescription(path, fallback string) string {
 if value, ok := fnSettingDescriptions[path]; ok { return value }; return fallback
}
func fnSettingValue(path, fallback string) string {
 if value, ok := fnSettingValues[path+\"\\x00\"+fallback]; ok { return value }; return fallback
}
func fnCompactNoun(noun string) string {
 if value, ok := fnCompactNouns[noun]; ok { return value }; return "?"
}
func fnActionDescription(fallback string) string {
 if value, ok := fnActionDescriptions[fallback]; ok { return value }
 for _, suffix := range []string{" (on a US layout)", " (built in, not configurable)"} {
  if strings.HasSuffix(fallback, suffix) {
   return fnActionDescription(strings.TrimSuffix(fallback, suffix))+fnActionDescriptions[suffix]
  }
 }
 return fallback
}
'''
    (directory / 'fntuios_display.go').write_text(funcs)
    for name, data in [('fnSettingLabels', labels), ('fnSettingDescriptions', descriptions), ('fnActionDescriptions', actions), ('fnSettingValues', values), ('fnCompactNouns', compact), ('fnRiskDisplay', risks)]:
        contents = 'package app\n\nvar ' + name + ' = map[string]string{\n'
        contents += ''.join(f' {quote(k)}: {quote(v)},\n' for k, v in sorted(data.items())) + '}\n'
        (directory / ('fntuios_' + name + '.go')).write_text(contents)
    guarded(directory / 'settings_registry.go', 'func settingLabel(path string) string {',
            'func settingLabel(path string) string {\n if label, ok := fnSettingLabels[path]; ok { return label }')
    guarded(directory / 'inbox_approvals_ext.go', 'printableTitle(name)+", "+inboxRiskWhy(name)',
            'printableTitle(fnRiskName(name))+", "+inboxRiskWhy(name)')
    guarded(directory / 'inbox_approvals_ext.go', 'if why, ok := risk.Why(name); ok {\n\t\treturn why',
            'if why, ok := risk.Why(name); ok {\n\t\treturn fnRiskText(why)')
    guarded(directory / 'settings_registry.go', 'Desc:    o.Description,',
            'Desc:    fnSettingDescription(path, o.Description),')
    guarded(directory / 'help.go', 'desc := b.Description', 'desc := fnBindingDescription(b.Action, b.Description)')
    guarded(directory / 'render_settings.go', 'overlay.Cycler(overlay.Truncate(item.value(m),',
            'overlay.Cycler(overlay.Truncate(fnSettingValue(item.Path, item.value(m)),')
    for file, name, expression in [
        ('render_sidebar_files.go', 'sidebarFilesLabelW', 'lipgloss.Width(sidebarFilesLabel)'),
        ('render_dock_notification.go', 'notifMoreWidth', '2 + lipgloss.Width(notifMoreLabel)'),
    ]:
        path = directory / file
        import re
        text, hits = re.subn(r'(?m)^\s*' + name + r'\s*=.*len\([^\n]+\)\n', '', path.read_text())
        if hits != 1:
            raise ValueError('localized cell-width adapter must match once: ' + name)
        path.write_text(text + '\nvar ' + name + ' = ' + expression + '\n')
    translated_save = next(e['translation'] for e in reviewed if e['source'] == 'Save to: ' and e['file'] == 'internal/app/multicopy_render.go')
    guarded(directory / 'multicopy_render.go', 'len(' + quote(translated_save) + ')', 'lipgloss.Width(' + quote(translated_save) + ')')
    guarded(directory / 'review_overlay.go', 'return strconv.Itoa(n) + " " + what + "s"', 'return strconv.Itoa(n) + " " + what')
    guarded(directory / 'help.go', 'func helpTabLabel(name string) string {\n\tif short, ok := helpTabNames[name]; ok {\n\t\treturn short\n\t}\n\treturn name\n}',
            'func helpTabLabel(name string) string {\n\tif short, ok := helpTabNames[name]; ok { return short }; return fnActionDescription(name)\n}')
    guarded(directory / 'sidebar_strip.go', 'g.noun+"s"', 'g.noun')
    guarded(Path(source) / 'internal/plural/plural.go', 'return CountAs(n, noun, noun+"s")',
            'for _, r := range noun { if r >= 0x3400 && r <= 0x9fff { return CountAs(n, noun, noun) } }; return CountAs(n, noun, noun+"s")')
    guarded(directory / 'sidebar_strip.go', 'return sidebarFit(sidebarStyle(bg, fg).Render(add)+sidebarStyle(bg, fg).Render(label), cw, bg)',
            'if lipgloss.Width(add)+lipgloss.Width(label)>cw { label=fnCompactNoun(noun) }; return sidebarFit(sidebarStyle(bg, fg).Render(add)+sidebarStyle(bg, fg).Render(label), cw, bg)')
    guarded(directory / 'sidebar_agent_tokens.go', 'return lead, true', 'return fnActionDescription(lead), true')
    guarded(directory / 'sidebar_agent_tokens.go', 'return kind, false', 'return fnActionDescription(kind), false')
    guarded(directory / 'accent_color.go', 'return accentSlotNames[clampInt(a.Slot, 0, accentSwatchCount-1)]',
            'return fnActionDescription(accentSlotNames[clampInt(a.Slot, 0, accentSwatchCount-1)])')
    guarded(directory / 'render_accent_picker.go', 'name := " " + accentSlotNames[s.Slot]',
            'name := " " + fnActionDescription(accentSlotNames[s.Slot])')
    human = next(e['translation'] for e in reviewed if e['source'] == 'you' and e['file'] == 'internal/app/agent_mail.go')
    guarded(directory / 'agent_mail.go', 'case "you", "human":', 'case "you", "human", ' + quote(human) + ':')

    guarded(directory / 'render_keybind_manager.go', 'text := a.Desc', 'text := fnBindingDescription(a.Action, a.Desc)')
    guarded(directory / 'render_keybind_manager.go', 'text = a.Desc +', 'text = fnBindingDescription(a.Action, a.Desc) +')
    guarded(directory / 'render_keybind_manager.go', '+fate.SwallowReason,', '+fnActionDescription(fate.SwallowReason),')
    config_display.apply(Path(source))
