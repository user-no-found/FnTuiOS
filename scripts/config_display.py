"""Connect native rendering to generated configuration display APIs."""


def replace_calls(path, old, new, count):
    text = path.read_text()
    if text.count(old) != count:
        raise ValueError(f'display API consumer changed in {path.name}: {old}')
    path.write_text(text.replace(old, new))


def apply(source):
    directory = source / 'internal/app'
    for file, old, new, count in [
        ('render_whichkey.go', 'config.PrefixMenuGroups(', 'fnPrefixMenuGroups(', 2),
        ('agents_seen.go', 'config.PrefixMenuGroups(', 'fnPrefixMenuGroups(', 1),
        ('factory.go', 'config.ConfigWarnings(cfg)', 'config.FnDisplayConfigWarnings(cfg)', 1),
        ('sidebar_custom.go', '.ResolvedTitle()', '.FnDisplayResolvedTitle()', 1),
        ('render_keybind_manager.go', 'config.Scopes(', 'config.FnDisplayScopes(', 1),
        ('render_keybind_manager.go', 'config.AmbiguityVerdict(', 'config.FnDisplayAmbiguityVerdict(', 1),
        ('keybind_manager.go', 'm.KeybindRegistry.Report(facts)', 'm.KeybindRegistry.FnDisplayReport(facts)', 1),
        ('keybind_manager.go', 'm.KeybindRegistry.Fate(', 'm.KeybindRegistry.FnDisplayFate(', 1),
        ('keybind_unbind.go', 'm.KeybindRegistry.Fate(', 'm.KeybindRegistry.FnDisplayFate(', 1),
        ('keybind_unbind.go', 'm.KeybindRegistry.StillHeldBy(', 'm.KeybindRegistry.FnDisplayStillHeldBy(', 1),
        ('render_keybind_manager.go', '+ c.ScopeName +', '+ fnActionDescription(c.ScopeName) +', 1),
    ]:
        replace_calls(directory / file, old, new, count)
