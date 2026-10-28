import { activateLauncher } from './panel_launcher.mjs';
import assert from 'node:assert/strict';
import { resolve } from 'node:path';
import { verifyPhoneControls } from './verify_panel_controls.mjs';
import { openPhoneKeyboard, verifyPhoneKeyboard } from './verify_mobile_keyboard.mjs';
import { verifyPanelDrag } from './verify_panel_drag.mjs';
import { verifyPhoneInput } from './verify_mobile_input.mjs';
import { verifyTextCopy } from './verify_text_copy.mjs';

async function until(check, message) {
  const deadline=Date.now()+15000;
  while(Date.now()<deadline){if(await check())return;await new Promise(done=>setTimeout(done,60));}
  throw new Error(message);
}

async function terminalText(frame) {
  return frame.evaluate(()=>{
    const buffer=window.sipTerm.term.buffer.active;
    return Array.from({length:buffer.length},(_,i)=>buffer.getLine(i)?.translateToString()||'').join('\n');
  });
}

// Touch/viewport emulation verifies the adapter; physical Android IME remains a device check.
export async function verifyMobile(browser, entry, output, desktop) {
  const grid=await desktop.evaluate(()=>({cols:window.sipTerm.term.cols,rows:window.sipTerm.term.rows}));
  const context=await browser.newContext({viewport:{width:390,height:844},isMobile:true,hasTouch:true,
    userAgent:'Mozilla/5.0 (Linux; Android 14; Mobile) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/155.0.0.0 Mobile Safari/537.36'});
  const page=await context.newPage();const errors=[];const input=[];
  page.on('pageerror',error=>errors.push(error.message));
  page.on('websocket',ws=>ws.on('framesent',({payload})=>{
    const bytes=Buffer.from(payload);if(bytes[0]===0x30)input.push(bytes.subarray(1).toString('hex'));
  }));
  try {
    await page.goto(entry.href,{waitUntil:'domcontentloaded'});
    await page.waitForFunction(()=>[...document.querySelectorAll('iframe')].some(f=>f.contentWindow?.sipTerm?.connected));
    const frame=page.frames().find(f=>new URL(f.url()||'about:blank').pathname.startsWith('/app/tuios/'));
    assert(frame,'mobile terminal frame absent');
    await frame.locator('#sip-keybar').waitFor();
    const drag=await verifyPanelDrag(page,frame,true);
    const keyboard = await verifyPhoneKeyboard(page, frame);
    const fixture=async(name,params={})=>{
      const url=new URL('/__fixture_image_'+name,entry);
      for(const [key,value] of Object.entries(params))url.searchParams.set(key,value);
      const response=await fetch(url);assert(response.ok);return response.json();
    };
    const beforeNew=await fixture('panes');let newPane;
    await frame.locator('#sip-keybar button').filter({hasText:/^新建$/}).tap();
    await until(async()=>{
      const panes=await fixture('panes');
      newPane=panes.windows.find(p=>!beforeNew.windows.some(old=>old.window_id===p.window_id));
      return newPane&&panes.focused_window_id===newPane.window_id;
    },'new phone pane did not acquire focus');
    await until(async()=>(await fixture('capture',{pane:newPane.window_id})).text.includes('$'),
      'new phone shell did not become ready');
    await openPhoneKeyboard(frame);
    await frame.evaluate(()=>window.sipTerm.webterm.focus());
    await page.keyboard.insertText("printf '\\x50HONE_UTF8_中文\\n'");
    await page.keyboard.press('Enter');
    await until(async()=>(await terminalText(frame)).includes('PHONE_UTF8_中文'),
      'mobile terminal did not execute UTF-8 input');
    const sidebar=frame.locator('#sip-keybar button').filter({hasText:/^侧栏$/});
    await sidebar.waitFor();await sidebar.tap();
    await until(async()=>{
      const text=await terminalText(frame);
      return text.includes('侧边栏')&&text.includes('会话')&&text.includes('文件');
    },'phone sidebar drawer did not expose session and file navigation');
    await page.screenshot({path:resolve(output,'mobile-sidebar.png'),animations:'disabled'});
    await frame.locator('#sip-keybar button').filter({hasText:/^esc$/i}).tap();
    await until(async()=>!(await terminalText(frame)).includes('侧边栏'),'phone sidebar did not return to terminal');
    const power=frame.getByRole('button',{name:'后端控制',exact:true});
    await power.waitFor();
    await until(async()=>!await power.isDisabled(),'shared mobile control launcher stayed disabled');
    const before=input.length;
    await activateLauncher(frame);await frame.getByRole('dialog').waitFor();
    assert.equal(await frame.evaluate(()=>window.FnMobileKeyboard.isOpen()),true);
    await page.waitForTimeout(100);
    const emitted=input.slice(before);
    assert(emitted.every(hex=>hex==='1b5b4f'||hex==='1b5b49'),
      'control launcher sent keystrokes instead of focus reports: '+JSON.stringify(emitted));
    await until(()=>frame.evaluate(()=>{
      const panel=document.querySelector('[data-fnpackkit-runtime]').shadowRoot.querySelector('.panel');
      return panel.getBoundingClientRect().bottom<=document.querySelector('#sip-keybar').getBoundingClientRect().top;
    }),'mobile control sheet overlaps terminal key bar');
    assert.equal(await power.isVisible(),true);
    await page.screenshot({path:resolve(output,'mobile-runtime-panel.png'),animations:'disabled'});
    const inputControls=await verifyPhoneControls(page,frame,entry,output);
    await frame.getByRole('button',{name:'关闭控制面板',exact:true}).tap();
    const ime=await verifyPhoneInput(page,frame);
    const copy=await verifyTextCopy(page,frame,entry,output,true);
    const rows=await frame.evaluate(()=>window.sipTerm.term.rows);
    await frame.evaluate(()=>{
      for(const [key,value] of Object.entries({width:390,height:460,offsetTop:0,offsetLeft:0})) {
        Object.defineProperty(window.visualViewport,key,{configurable:true,value});
      }
      window.visualViewport.dispatchEvent(new Event('resize'));
      window.dispatchEvent(new Event('resize'));
    });
    await until(()=>frame.evaluate(previous=>window.sipTerm.term.rows<previous,rows),
      'terminal did not refit after simulated keyboard geometry');
    await activateLauncher(frame);await frame.getByRole('dialog').waitFor();
    await until(()=>frame.evaluate(()=>{
      const rect=document.querySelector('[data-fnpackkit-runtime]').shadowRoot.querySelector('.panel').getBoundingClientRect();
      return rect.top>=0&&rect.bottom<=460;
    }),'phone control sheet escaped the visual viewport');
    await frame.getByRole('button',{name:'关闭控制面板',exact:true}).tap();
    await frame.evaluate(()=>{
      for(const key of ['width','height','offsetTop','offsetLeft'])delete window.visualViewport[key];
      window.visualViewport.dispatchEvent(new Event('resize'));
      window.dispatchEvent(new Event('resize'));
    });
    await page.setViewportSize({width:844,height:390});
    await activateLauncher(frame);await frame.getByRole('dialog').waitFor();
    await until(()=>frame.evaluate(()=>{
      const panel=document.querySelector('[data-fnpackkit-runtime]').shadowRoot.querySelector('.panel').getBoundingClientRect();
      return panel.left>=0&&panel.right<=innerWidth&&panel.bottom<=innerHeight;
    }),'landscape controls are not reachable');
    assert.deepEqual(await desktop.evaluate(()=>({cols:window.sipTerm.term.cols,rows:window.sipTerm.term.rows})),grid,
      'phone presentation changed desktop terminal grid');
    assert.deepEqual(errors,[]);
    return {drag,keyboard,inputControls,ime,copy,navigation:'passed',utf8Input:'browser-insertion-passed',toolbarControls:'passed',controlKeyIsolation:'passed',focusReports:emitted,
      keyboardGeometry:'simulated-passed',landscape:'passed',desktopGrid:'passed',pageErrors:0};
  } catch(error) {
    await page.screenshot({path:resolve(output,'mobile-failure.png'),animations:'disabled'});
    throw error;
  } finally {await context.close();}
}
