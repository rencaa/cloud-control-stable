import{aj as o,aU as s,cx as a,cy as i}from"./index-CNNfIvlU.js";import{y as u,A as c,u as d}from"./vue-vendor-B7FFCbTC.js";const r={prefix:Math.floor(Math.random()*1e4),current:0},I=Symbol("elIdInjection"),p=()=>u()?c(I,r):r,j=n=>{const e=p();!o&&e===r&&s("IdInjection",`Looks like you are using server rendering, you must provide a id provider to ensure the hydration process to be succeed
usage: app.provide(ID_INJECTION_KEY, {
  prefix: number,
  current: number,
})`);const t=a();return i(()=>d(n)||`${t.value}-id-${e.prefix}-${e.current++}`)};export{p as a,j as u};
