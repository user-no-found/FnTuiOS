import { activateLauncher } from './panel_launcher.mjs';
import assert from 'node:assert/strict';
import { resolve } from 'node:path';
import { openPhoneKeyboard } from './verify_mobile_keyboard.mjs';

const PNG=Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAMAAAACCAYAAACddGYaAAAAE0lEQVR4nGOs0NjynwEKmBiQAAA13AJX+/hyRQAAAABJRU5ErkJggg==','base64');
const pause=ms=>new Promise(done=>setTimeout(done,ms));
async function until(check,message){const end=Date.now()+15000;while(Date.now()<end){if(await check())return;await pause(70);}throw new Error(message);}
function fixtures(entry){return async(name,params={},method='GET')=>{
  const url=new URL('/__fixture_image_'+name,entry);
  for(const [k,v]of Object.entries(params))url.searchParams.set(k,v);
  const response=await fetch(url,{method});assert(response.ok,`fixture ${name}: ${response.status}`);return response.json();
};}
export async function verifySplits(page,frame,entry,output){
  const fixture=fixtures(entry);
  const paneID=w=>w.window_id||w.id;
  await fixture('action',{action:'new'},'POST');
  await until(async()=>(await fixture('panes')).windows.length>0,'desktop pane missing');
  const results=[];
  for(const [direction,label]of [['up','上'],['down','下'],['left','左'],['right','右']]){
    await activateLauncher(frame);await frame.getByRole('button',{name:'上分屏',exact:true}).waitFor();
    const geometry=await frame.evaluate(()=>{
      const root=document.querySelector('[data-fnpackkit-runtime]').shadowRoot;
      const rect=selector=>{const r=root.querySelector(selector).getBoundingClientRect();return {x:r.x,y:r.y,width:r.width,height:r.height,right:r.right};};
      return {start:rect('.actions button:first-child'),end:rect('.actions button:last-child'),settings:rect('[data-action="terminal-settings"]'),copy:rect('[data-action="copy-text"]'),
        splits:['up','down','left','right'].map(d=>rect(`[data-action="split-${d}"]`))};
    });
    for(const index of [0,2])assert(Math.abs(geometry.splits[index].x-geometry.start.x)<1);
    for(const index of [1,3])assert(Math.abs(geometry.splits[index].right-geometry.end.right)<1);
    for(const r of geometry.splits){assert(Math.abs(r.width-geometry.splits[0].width)<1);assert.equal(r.height,geometry.splits[0].height);}
    assert.equal(geometry.splits[0].y,geometry.splits[1].y);assert.equal(geometry.splits[2].y,geometry.splits[3].y);
    assert(Math.abs(geometry.settings.x-geometry.start.x)<1&&Math.abs(geometry.copy.right-geometry.end.right)<1);
    assert.equal(geometry.settings.y,geometry.copy.y);assert(Math.abs(geometry.settings.width-geometry.copy.width)<1);
    assert.equal(await frame.getByRole('button',{name:'添加照片',exact:true}).count(),0);
    const before=await fixture('panes');const oldID=before.focused_window_id;
    await frame.getByRole('button',{name:label+'分屏',exact:true}).click();
    let pair;
    await until(async()=>{
      const after=await fixture('panes');
      const added=after.windows.find(w=>!before.windows.some(p=>paneID(p)===paneID(w)));
      const original=after.windows.find(w=>paneID(w)===oldID);
      if(!added||!original)return false;
      const vertical=direction==='up'||direction==='down';
      const a=vertical?added.y:added.x,b=vertical?original.y:original.x;
      if(!Number.isFinite(a)||!Number.isFinite(b)||a===b)return false;
      const ordered=['up','left'].includes(direction)?a<b:a>b;
      if(ordered)pair={added,original};return ordered;
    },'new pane did not split '+direction);
    results.push({direction,...pair,geometry});
    await fixture('action',{action:'close',pane:paneID(pair.added)},'POST');
    await fixture('action',{action:'focus',pane:oldID},'POST');
    await until(async()=>(await fixture('panes')).windows.length===before.windows.length,'split pane not removed');
  }
  await activateLauncher(frame);await page.screenshot({path:resolve(output,'desktop-split-controls.png'),animations:'disabled'});
  await frame.getByRole('button',{name:'关闭控制面板',exact:true}).click();
  return results;
}

export async function verifyPhoneControls(page,frame,entry,output){
  const fixture=fixtures(entry);
  const button=name=>frame.getByRole('button',{name,exact:true});
  // Existing sockets predate this function; observe their transport without replacing it.
  await frame.evaluate(()=>{
    window.__panelFrames=[];
    const conn=window.sipTerm.connection,send=conn.sendMessage.bind(conn);
    window.__panelRestore=()=>{conn.sendMessage=send;};
    conn.sendMessage=(type,payload)=>{if(type===0x30)window.__panelFrames.push(new TextDecoder().decode(payload));return send(type,payload);};
  });
  const read=()=>frame.evaluate(()=>window.__panelFrames.splice(0).join(''));
  const arrows=text=>text.match(/\x1b\[(?:1;[0-9]+(?::[123])?)?[ABCD]/g)||[];
  const matches=[];
  try{
    assert.equal(await button('上分屏').count(),0);
    for(const protocol of ['negotiated','report-events']){
    if(protocol==='report-events'){
      await frame.evaluate(()=>window.sipTerm.webterm.write('\x1b[>3u'));
      await frame.waitForFunction(()=>window.sipTerm.webterm.keyboard.flags===3);
    }
    for(let mask=0;mask<8;mask++)for(const [dir,label]of [['Up','↑'],['Down','↓'],['Left','←'],['Right','→']]){
      if(await frame.getByRole('dialog').isVisible())await button('关闭控制面板').tap();
      await openPhoneKeyboard(frame);
      await frame.evaluate(()=>window.sipTerm.webterm.focus());await read();
      const mods=['Control','Shift','Alt'].filter((_,i)=>mask&(1<<i));
      await page.keyboard.press([...mods,'Arrow'+dir].join('+'));await pause(60);
      const reference=arrows(await read());assert(reference.length,'physical arrow reference absent');
      await activateLauncher(frame);await frame.getByRole('dialog').waitFor();
      for(const mod of mods)await button(mod==='Control'?'Ctrl':mod).tap();
      for(const mod of mods)assert.equal(await button(mod==='Control'?'Ctrl':mod).getAttribute('aria-pressed'),'true');
      await read();await button(label).tap();await pause(60);
      const actual=arrows(await read());assert.deepEqual(actual,reference,`${mods.join('+')}+Arrow${dir}`);
      for(const mod of ['Ctrl','Shift','Alt'])assert.equal(await button(mod).getAttribute('aria-pressed'),'false');
      assert(await frame.getByRole('dialog').isVisible(),'arrow unexpectedly closed panel');
      matches.push({protocol,mask,direction:dir,bytes:actual});
    }
    if(protocol==='report-events')await frame.evaluate(()=>window.sipTerm.webterm.write('\x1b[<u'));
    }
    await frame.evaluate(()=>{window.sipTerm.readOnly=true;window.FnPanelControls.sync(window.sipTerm);});
    for(const name of ['Ctrl','Shift','Alt','↑','↓','←','→','添加照片','键盘'])assert(await button(name).isDisabled());
    await frame.evaluate(()=>{window.sipTerm.readOnly=false;window.FnPanelControls.sync(window.sipTerm);});
    await frame.evaluate(()=>{window.sipTerm.connected=false;window.FnPanelControls.sync(window.sipTerm);});
    for(const name of ['Ctrl','Shift','Alt','↑','↓','←','→','添加照片','键盘'])assert(await button(name).isDisabled());
    await frame.evaluate(()=>{window.sipTerm.connected=true;window.FnPanelControls.sync(window.sipTerm);});
    const reader=(await fixture('action',{action:'reader'},'POST')).window_id;
    await until(async()=>(await fixture('capture',{pane:reader})).text.includes('FN_IMAGE_READY'),'photo reader missing');
    const before=(await fixture('files')).files;
    for(let i=0;i<2;i++){
      if(!await frame.getByRole('dialog').isVisible())await activateLauncher(frame);
      const selected=page.waitForEvent('filechooser');await button('添加照片').tap();
      await (await selected).setFiles({name:'same-photo.png',mimeType:'image/png',buffer:PNG});
      await until(async()=>(await fixture('files')).files.length===before.length+i+1,'photo did not create image');
      await until(async()=>(await frame.locator('#fn-image-paste-status').textContent())===await frame.evaluate(()=>window.fnImagePasteMessages.complete),'photo delivery not confirmed');
      assert.equal(await frame.evaluate(()=>window.FnMobileKeyboard.isOpen()),false,'photo completion reopened the keyboard');
    }
    const created=(await fixture('files')).files.filter(f=>!before.some(p=>p.path===f.path));
    const text=(await fixture('capture',{pane:reader})).text.replace(/[ \t]*\n/g,'');
    for(const image of created){assert.equal(image.mode,'0600');assert(text.includes(image.path),'photo path absent from captured pane');}
    assert(!text.includes('FN_IMAGE_SUBMITTED'),'photo submitted the terminal line');
    await activateLauncher(frame);await button('Ctrl').tap();
    const selected=page.waitForEvent('filechooser');await button('添加照片').tap();await selected;
    await frame.locator('input[type=file]').setInputFiles([]);
    await pause(150);assert.equal((await fixture('files')).files.length,before.length+2,'cancel uploaded a file');
    await activateLauncher(frame);assert.equal(await button('Ctrl').getAttribute('aria-pressed'),'false');
    await page.screenshot({path:resolve(output,'mobile-input-controls.png'),animations:'disabled'});
    return {modifierArrows:matches,photoUploads:created,cancel:'passed',readOnly:'passed',disconnected:'passed',sameFileReselect:'passed'};
  }finally{await frame.evaluate(()=>{window.__panelRestore?.();delete window.__panelRestore;delete window.__panelFrames;});}
}
