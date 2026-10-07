import type { Register } from 'claude-code'

import { register as wire } from '../hosts/claude-code/mod/register.tsx'

export const register: Register = (on, options) => wire(on, options)
