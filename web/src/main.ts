import { mount } from 'svelte'
import './boot'
import App from './App.svelte'

mount(App, { target: document.getElementById('app')! })

if ('serviceWorker' in navigator) navigator.serviceWorker.register('/sw.js').catch(() => {})
