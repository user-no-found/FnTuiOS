import assert from 'node:assert/strict';
import { openPhoneKeyboard } from './verify_mobile_keyboard.mjs';

// CDP exercises Chromium's composition DOM. Event fixtures cover alternate
// mobile commit/delete forms; neither is a physical Android IME claim.
export async function verifyPhoneInput(page, frame) {
  await openPhoneKeyboard(frame);
  await frame.evaluate(() => {
    const connection = window.sipTerm.connection;
    const send = connection.sendMessage;
    window.__fnInputCheck = { wire: [], restore: () => { connection.sendMessage = send; } };
    connection.sendMessage = function (type, bytes) {
      if (type === 0x30) window.__fnInputCheck.wire.push(new TextDecoder().decode(bytes));
      return send.call(this, type, bytes);
    };
  });
  const checks = [];
  const cdp = await page.context().newCDPSession(page);
  const clear = () => frame.evaluate(() => { window.__fnInputCheck.wire = []; });
  async function expectWire(expected, label) {
    await page.waitForTimeout(100);
    const actual = await frame.evaluate(() => window.__fnInputCheck.wire.join('').replace(/\x1b\[[IO]/g, ''));
    assert.equal(actual, expected, label);
    checks.push(label);
  }
  async function composition(data, cancel = false, trailing = '') {
    await frame.evaluate(({ data, cancel, trailing }) => {
      const t = window.sipTerm.webterm.xterm.textarea;
      t.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true, data: '' }));
      t.dispatchEvent(new CompositionEvent('compositionupdate', { bubbles: true, data: 'zhongwen' }));
      // A mobile commit can carry data after its editable value was cleared.
      t.value = '';
      t.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: cancel ? '' : data }));
      if (trailing) t.dispatchEvent(new InputEvent('input', { bubbles: true, data, inputType: trailing }));
    }, { data, cancel, trailing });
  }
  async function deleteEdit({ before = true, cancelable = true, unknownKey = false, physical = false } = {}) {
    await frame.evaluate(options => {
      const t = window.sipTerm.webterm.xterm.textarea;
      t.value = '';
      if (options.unknownKey) t.dispatchEvent(new KeyboardEvent('keydown', {
        bubbles: true, key: 'Unidentified', keyCode: 229, code: '', cancelable: true,
      }));
      if (options.physical) t.dispatchEvent(new KeyboardEvent('keydown', {
        bubbles: true, key: 'Backspace', code: 'Backspace', keyCode: 8, cancelable: true,
      }));
      const proceed = !options.before || t.dispatchEvent(new InputEvent('beforeinput', {
        bubbles: true, cancelable: options.cancelable, inputType: 'deleteContentBackward',
      }));
      if (proceed) t.dispatchEvent(new InputEvent('input', {
        bubbles: true, inputType: 'deleteContentBackward',
      }));
      if (options.unknownKey || options.physical) t.dispatchEvent(new KeyboardEvent('keyup', {
        bubbles: true, key: options.physical ? 'Backspace' : 'Unidentified',
        code: options.physical ? 'Backspace' : '', keyCode: options.physical ? 8 : 229,
      }));
    }, { before, cancelable, unknownKey, physical });
  }
  async function pasteEdit(kind, data, preedit = false) {
    await frame.evaluate(({ kind, data, preedit }) => {
      const t = window.sipTerm.webterm.xterm.textarea;
      if (preedit) {
        t.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }));
        t.value = 'weiwancheng';
      }
      if (kind === 'clipboard-event') {
        const clipboardData = new DataTransfer(); clipboardData.setData('text/plain', data);
        t.dispatchEvent(new ClipboardEvent('paste', { bubbles: true, cancelable: true, clipboardData }));
      } else {
        if (kind === 'beforeinput') t.dispatchEvent(new InputEvent('beforeinput', {
          bubbles: true, cancelable: true, inputType: 'insertFromPaste', data,
        }));
        t.value = data;
        t.dispatchEvent(new InputEvent('input', {
          bubbles: true, composed: true, inputType: 'insertFromPaste', data: kind === 'null-data' ? null : data,
        }));
      }
      if (preedit) t.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: '未完成' }));
    }, { kind, data, preedit });
  }
  try {
    await clear();
    await cdp.send('Input.imeSetComposition', { text: 'nihao', selectionStart: 5, selectionEnd: 5 });
    await expectWire('', 'native Chromium preedit sends no unfinished pinyin');
    await cdp.send('Input.insertText', { text: '你好' });
    await expectWire('你好', 'native Chromium composition commits Chinese once');

    await clear();
    await frame.evaluate(() => {
      const t = window.sipTerm.webterm.xterm.textarea;
      t.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'Unidentified', keyCode: 229 }));
      t.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }));
      t.value = 'nihao';
      t.dispatchEvent(new InputEvent('input', {
        bubbles: true, composed: true, data: 'nihao', inputType: 'insertText', isComposing: true,
      }));
    });
    await expectWire('', 'Android 229 preedit insertText never leaks unfinished pinyin');
    await frame.evaluate(() => window.sipTerm.webterm.xterm.textarea.dispatchEvent(
      new CompositionEvent('compositionend', { bubbles: true, data: '你好' })));
    await expectWire('你好', 'Android preedit becomes one final Chinese commit');
    await clear();
    await frame.evaluate(() => {
      const t = window.sipTerm.webterm.xterm.textarea;
      t.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'Unidentified', keyCode: 0 }));
      t.value = '隐式预编辑';
      t.dispatchEvent(new InputEvent('input', {
        bubbles: true, composed: true, data: '隐式预编辑', inputType: 'insertCompositionText', isComposing: true,
      }));
    });
    await expectWire('', 'IME without compositionstart still keeps preedit local');
    await frame.evaluate(() => {
      const t = window.sipTerm.webterm.xterm.textarea;
      t.value = '隐式中文';
      t.dispatchEvent(new InputEvent('input', {
        bubbles: true, composed: true, data: '隐式中文', inputType: 'insertText',
      }));
    });
    await expectWire('隐式中文', 'IME without compositionend commits its final input event once');

    await clear(); await composition('中文');
    await expectWire('中文', 'event-data commit survives an empty helper value');
    await clear(); await composition('中文'); await composition('中文');
    await expectWire('中文中文', 'identical consecutive Chinese commits are both retained');
    await clear(); await composition('', true);
    await expectWire('', 'cancelled composition emits no terminal text');
    for (const type of ['insertText', 'insertFromComposition']) {
      await clear(); await composition('选词', false, type);
      await expectWire('选词', `trailing ${type} notification does not duplicate the commit`);
    }

    await clear();
    await frame.evaluate(() => {
      const c = window.sipTerm, t = c.webterm.xterm.textarea;
      t.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }));
      t.value = '失焦中文'; t.blur();
      t.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: '失焦中文' }));
      c.webterm.focus();
    });
    await expectWire('失焦中文', 'transient IME blur preserves the final commit and keyboard authorization');
    assert.equal(await frame.evaluate(() => window.FnMobileKeyboard.isOpen()), true);

    for (const bracketed of [false, true]) {
      await frame.evaluate(on => window.sipTerm.webterm.write(on ? '\x1b[?2004h' : '\x1b[?2004l'), bracketed);
      await frame.waitForFunction(on => window.sipTerm.webterm.xterm.modes.bracketedPasteMode === on, bracketed);
      const data = '输入法粘贴中文\n第二行';
      const expected = (bracketed ? '\x1b[200~' : '') + data.replace(/\n/g, '\r') + (bracketed ? '\x1b[201~' : '');
      for (const kind of ['input-only', 'beforeinput', 'null-data', 'clipboard-event']) {
        await clear(); await pasteEdit(kind, data);
        await expectWire(expected, `IME ${kind} paste preserves Chinese, newlines and bracketed mode ${bracketed}`);
      }
      for (const kind of ['input-only', 'beforeinput', 'clipboard-event']) {
        await clear(); await pasteEdit(kind, data, true);
        await expectWire(expected, `IME ${kind} paste discards unfinished preedit (bracketed mode ${bracketed})`);
      }
      await page.context().grantPermissions(['clipboard-read', 'clipboard-write'], {
        origin: new URL(frame.url()).origin,
      });
      await frame.evaluate(() => window.sipTerm.webterm.write('\x1b[>0u'));
      await frame.waitForFunction(() => window.sipTerm.webterm.keyboard.flags === 0);
      await frame.evaluate(text => navigator.clipboard.writeText(text), data);
      await clear(); await page.keyboard.press('Control+V');
      await expectWire(expected, `trusted browser clipboard paste remains intact (bracketed mode ${bracketed})`);
      await frame.evaluate(() => window.sipTerm.webterm.write('\x1b[<u'));
    }
    await frame.evaluate(() => window.sipTerm.webterm.write('\x1b[?2004l'));

    await clear();
    await frame.evaluate(() => {
      const t = window.sipTerm.webterm.xterm.textarea;
      t.value = ''; t.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }));
      t.value = 'zhongwen';
      t.dispatchEvent(new KeyboardEvent('keydown', {
        bubbles: true, cancelable: true, key: 'Enter', code: 'Enter', keyCode: 13, isComposing: true,
      }));
      t.dispatchEvent(new InputEvent('beforeinput', {
        bubbles: true, cancelable: true, inputType: 'deleteContentBackward', isComposing: true,
      }));
    });
    await expectWire('', 'candidate Enter and candidate deletion do not submit or erase terminal input');
    await frame.evaluate(() => {
      const t = window.sipTerm.webterm.xterm.textarea;
      t.value = '中文'; t.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: '中文' }));
    });
    await expectWire('中文', 'candidate selection commits only its final Chinese text');

    for (const flags of [0, 3, 31]) {
      await frame.evaluate(flags => window.sipTerm.webterm.write(`\x1b[>${flags}u`), flags);
      await frame.waitForFunction(flags => window.sipTerm.webterm.keyboard.flags === flags, flags);
      await clear();
      await frame.evaluate(() => {
        const t = window.sipTerm.webterm.xterm.textarea;
        t.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }));
        t.dispatchEvent(new KeyboardEvent('keydown', {
          bubbles: true, cancelable: true, key: 'Enter', code: 'Enter', keyCode: 13, isComposing: true,
        }));
        t.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: '候选中文' }));
        t.dispatchEvent(new KeyboardEvent('keyup', {
          bubbles: true, key: 'Enter', code: 'Enter', keyCode: 13,
        }));
      });
      await expectWire('候选中文', `candidate Enter releases no stray terminal key (flags ${flags})`);
      const expected = await frame.evaluate(() => {
        const flags = window.sipTerm.webterm.keyboard.flags;
        const e = type => new KeyboardEvent(type, { key: 'Backspace', code: 'Backspace' });
        return (window.WebTerm.encodeKey(e('keydown'), flags) ?? '\x7f')
          + (window.WebTerm.encodeKey(e('keyup'), flags) || '');
      });
      for (const [name, options] of [
        ['beforeinput delete', {}],
        ['input-only delete', { before: false }],
        ['noncancelable delete pair', { cancelable: false }],
        ['Android 229 delete', { unknownKey: true }],
        ['physical plus edit notification', { physical: true }],
      ]) {
        await clear(); await deleteEdit(options);
        await expectWire(expected, `${name} sends one protocol-correct deletion (flags ${flags})`);
      }
      await clear(); await page.keyboard.press('Backspace');
      await expectWire(expected, `real browser Backspace stays on the native key path (flags ${flags})`);
      await clear();
      await frame.evaluate(() => {
        const t = window.sipTerm.webterm.xterm.textarea;
        for (let i = 0; i < 25; i++) {
          assertAnchor(); t.value = '';
          t.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'deleteContentBackward' }));
        }
        assertAnchor();
        function assertAnchor() {
          if (t.value.length !== 1 || t.selectionStart !== 1) throw new Error('mobile editable cannot accept another soft deletion');
        }
      });
      await expectWire(expected.repeat(25), `held soft deletion remains editable for 25 operations (flags ${flags})`);
      await clear();
      const forward = await frame.evaluate(() => {
        const flags = window.sipTerm.webterm.keyboard.flags;
        const e = type => new KeyboardEvent(type, { key: 'Delete', code: 'Delete' });
        const expected = (window.WebTerm.encodeKey(e('keydown'), flags) ?? '\x1b[3~')
          + (window.WebTerm.encodeKey(e('keyup'), flags) || '');
        window.sipTerm.webterm.xterm.textarea.dispatchEvent(new InputEvent('beforeinput', {
          bubbles: true, cancelable: true, inputType: 'deleteContentForward',
        }));
        return expected;
      });
      await expectWire(forward, `forward deletion uses the negotiated protocol (flags ${flags})`);
      await frame.evaluate(() => window.sipTerm.webterm.write('\x1b[<u'));
    }

    await clear(); await cdp.send('Input.insertText', { text: 'ab中文' });
    await expectWire('ab中文', 'ordinary mobile text remains unchanged');
    await frame.evaluate(() => window.sipTerm.webterm.write('\x1b[>0u'));
    await frame.waitForFunction(() => window.sipTerm.webterm.keyboard.flags === 0);
    await clear(); await page.keyboard.type('Aa1');
    await expectWire('Aa1', 'physical uppercase, lowercase and digits keep native encoding without duplicate input');
    await clear(); await page.keyboard.press('Enter');
    await expectWire('\r', 'Enter outside composition still submits normally');
    await frame.evaluate(() => window.sipTerm.webterm.write('\x1b[<u'));

    for (const mode of ['closed', 'readOnly', 'disconnected']) {
      await clear();
      await frame.evaluate(mode => {
        const c = window.sipTerm;
        if (mode === 'readOnly') c.readOnly = true;
        if (mode === 'disconnected') c.connected = false;
        window.FnMobileKeyboard.close(c);
      }, mode);
      await composition('不得送出'); await deleteEdit();
      await frame.evaluate(() => window.sipTerm.webterm.xterm.textarea.dispatchEvent(new InputEvent('input', {
        bubbles: true, data: '不得粘贴', inputType: 'insertFromPaste',
      })));
      await expectWire('', `${mode} rejects mobile composition, deletion and paste`);
      await frame.evaluate(() => {
        const c = window.sipTerm; c.readOnly = false; c.connected = true;
        window.FnPanelControls.sync(c);
      });
      await openPhoneKeyboard(frame);
    }
    return { checks, boundary: 'real Chromium composition and synthetic mobile edit forms; physical IME not exercised' };
  } finally {
    await cdp.detach();
    await frame.evaluate(() => { window.__fnInputCheck.restore(); delete window.__fnInputCheck; });
  }
}
