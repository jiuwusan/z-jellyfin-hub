import test from 'node:test'
import assert from 'node:assert/strict'
import {copyText} from '../src/clipboard.ts'

test('clipboard success, HTTP fallback and refusal clean up transient secrets',async()=>{
 const nav=Object.getOwnPropertyDescriptor(globalThis,'navigator'),doc=Object.getOwnPropertyDescriptor(globalThis,'document')
 let copied='',removed=0,focused=0
 const field={value:'',style:{},focus(){},select(){},remove(){removed++}}
 const document={activeElement:{focus(){focused++}},body:{appendChild(){}},createElement(){return field},execCommand(command){assert.equal(command,'copy');copied=field.value;return true}}
 try{
  Object.defineProperty(globalThis,'navigator',{configurable:true,value:{clipboard:{async writeText(value){copied=value}}}})
  assert.equal(await copyText('fixture-link'),true);assert.equal(copied,'fixture-link')
  Object.defineProperty(globalThis,'navigator',{configurable:true,value:{}})
  Object.defineProperty(globalThis,'document',{configurable:true,value:document})
  assert.equal(await copyText('fixture-http-link'),true);assert.equal(copied,'fixture-http-link');assert.equal(removed,1)
  document.execCommand=()=>false
  assert.equal(await copyText('fixture-refused'),false);assert.equal(removed,2);assert.equal(focused,2)
  document.execCommand=()=>{throw new Error('Clipboard blocked')}
  assert.equal(await copyText('fixture-blocked'),false);assert.equal(removed,3)
 }finally{
  if(nav)Object.defineProperty(globalThis,'navigator',nav);else delete globalThis.navigator
  if(doc)Object.defineProperty(globalThis,'document',doc);else delete globalThis.document
 }
})
