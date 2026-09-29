import { mount } from 'svelte'
import './boot'
import App from './App.svelte'

mount(App, { target: document.getElementById('app')! })
