<script lang="ts">import { bytes } from '../format';let { value = 0 }: { value?: number } = $props();</script><span title={`${value} bytes`}>{bytes(value)}</span>
