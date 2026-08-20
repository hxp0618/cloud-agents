import { create, fromBinary, toBinary } from "@bufbuild/protobuf";
import { Code, ConnectError, createClient, type Transport } from "@connectrpc/connect";
import { describe, expect, it } from "vitest";

import {
  PlatformAdapterExecutionService,
  PlatformAdapterRegistryService,
  WorkerExecutionService,
} from "./proto.js";
import { NegotiationRequestSchema } from "./gen/contracts/worker/v1alpha1/kernel_pb.js";

const stableDeniedMessage = "fixture denied: no production side effect";

function fixtureTransport() {
  const calls: string[] = [];
  let deny = false;
  let observedTimeout: number | undefined;
  const transport: Transport = {
    async unary(method, signal, timeoutMs) {
      calls.push(`${method.parent.typeName}/${method.name}`);
      observedTimeout = timeoutMs;
      if (signal?.aborted) {
        throw new ConnectError("fixture canceled", Code.Canceled);
      }
      if (timeoutMs !== undefined && timeoutMs <= 1) {
        throw new ConnectError("fixture deadline", Code.DeadlineExceeded);
      }
      if (deny) {
        throw new ConnectError(stableDeniedMessage, Code.PermissionDenied);
      }
      return {
        stream: false,
        service: method.parent,
        method,
        header: new Headers(),
        trailer: new Headers(),
        message: create(method.output),
      } as never;
    },
    async stream() {
      throw new ConnectError("streaming is not part of this generated profile", Code.Unimplemented);
    },
  };
  return {
    transport,
    calls,
    setDeny(value: boolean) {
      deny = value;
    },
    getObservedTimeout() {
      return observedTimeout;
    },
  };
}

describe("generated proto SDK", () => {
  it("covers every generated unary method through the injected transport", async () => {
    const fixture = fixtureTransport();
    const worker = createClient(WorkerExecutionService, fixture.transport);
    const registry = createClient(PlatformAdapterRegistryService, fixture.transport);
    const execution = createClient(PlatformAdapterExecutionService, fixture.transport);

    await worker.negotiate({});
    await worker.checkHealth({});
    await worker.executeOperation({});
    await worker.getOperationReceipt({});
    await registry.negotiate({});
    await registry.registerAdapter({});
    await registry.getAdapterRegistrationReceipt({});
    await execution.negotiate({});
    await execution.getCapabilities({});
    await execution.checkHealth({});
    await execution.executeOperation({});
    await execution.getOperationReceipt({});

    expect(fixture.calls).toEqual([
      "cloudagents.worker.v1alpha1.WorkerExecutionService/Negotiate",
      "cloudagents.worker.v1alpha1.WorkerExecutionService/CheckHealth",
      "cloudagents.worker.v1alpha1.WorkerExecutionService/ExecuteOperation",
      "cloudagents.worker.v1alpha1.WorkerExecutionService/GetOperationReceipt",
      "cloudagents.platformadapter.v1alpha1.PlatformAdapterRegistryService/Negotiate",
      "cloudagents.platformadapter.v1alpha1.PlatformAdapterRegistryService/RegisterAdapter",
      "cloudagents.platformadapter.v1alpha1.PlatformAdapterRegistryService/GetAdapterRegistrationReceipt",
      "cloudagents.platformadapter.v1alpha1.PlatformAdapterExecutionService/Negotiate",
      "cloudagents.platformadapter.v1alpha1.PlatformAdapterExecutionService/GetCapabilities",
      "cloudagents.platformadapter.v1alpha1.PlatformAdapterExecutionService/CheckHealth",
      "cloudagents.platformadapter.v1alpha1.PlatformAdapterExecutionService/ExecuteOperation",
      "cloudagents.platformadapter.v1alpha1.PlatformAdapterExecutionService/GetOperationReceipt",
    ]);
  });

  it("preserves cancellation, deadline and stable error codes/messages", async () => {
    const fixture = fixtureTransport();
    const worker = createClient(WorkerExecutionService, fixture.transport);
    const controller = new AbortController();
    controller.abort();
    await expect(worker.negotiate({}, { signal: controller.signal })).rejects.toMatchObject({
      code: Code.Canceled,
    });

    await expect(worker.checkHealth({}, { timeoutMs: 1 })).rejects.toMatchObject({
      code: Code.DeadlineExceeded,
    });
    expect(fixture.getObservedTimeout()).toBe(1);

    fixture.setDeny(true);
    await expect(worker.getOperationReceipt({})).rejects.toMatchObject({
      code: Code.PermissionDenied,
      rawMessage: stableDeniedMessage,
    });
  });

  it("preserves unknown protobuf fields across binary decode and encode", () => {
    const encoded = Uint8Array.from([0xd8, 0x07, 0x01]); // field 123, wire type 0, value 1
    const request = fromBinary(NegotiationRequestSchema, encoded);
    expect(request.$unknown).toEqual([{ no: 123, wireType: 0, data: Uint8Array.from([0x01]) }]);
    expect(Array.from(toBinary(NegotiationRequestSchema, request))).toEqual(Array.from(encoded));
  });
});
