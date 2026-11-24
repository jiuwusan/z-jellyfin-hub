/** Only called after an explicit admin click; never persists or logs the copied secret. */
export async function copyText(value:string):Promise<boolean>{
 try{if(globalThis.navigator?.clipboard){await navigator.clipboard.writeText(value);return true}}catch{/* HTTP or browser policy may require the fallback. */}
 const document=globalThis.document
 if(!document)return false
 const previous=document.activeElement as HTMLElement|null
 const field=document.createElement('textarea')
 field.value=value;field.readOnly=true
 field.style.position='fixed';field.style.opacity='0';field.style.pointerEvents='none'
 try{
  document.body.appendChild(field);field.focus();field.select()
  return document.execCommand('copy')
 }catch{return false}
 finally{field.remove();previous?.focus?.({preventScroll:true})}
}
