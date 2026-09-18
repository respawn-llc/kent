import { act, renderHook, waitFor } from "@testing-library/react";
import { createTestServices } from "@/test-support/app-services";
import { composerWrapper } from "@/test-support/composer";
import { useChatComposer, target } from "./chatComposerTestFixture";
import { createChatStorageFixture } from "./chatStorageFixture";

beforeEach(() => vi.stubGlobal("localStorage", createChatStorageFixture()));
afterEach(() => vi.unstubAllGlobals());

it("places a late saved draft before typing without losing exact whitespace", async () => {
  const services = createTestServices([]);
  let deliver!: (text: string) => void;
  vi.spyOn(services.api.chat, "getDraft").mockReturnValue(
    new Promise<Awaited<ReturnType<typeof services.api.chat.getDraft>>>((resolve) => {
      deliver = (input) => {
        resolve({ input, protectedInput: null });
      };
    }),
  );
  const save = vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  const { result } = renderHook(() => useChatComposer({ ...target }), {
    wrapper: composerWrapper(services),
  });
  act(() => {
    result.current.edit(" new\ntext ");
  });
  expect(result.current.draft.kind).toBe("loading");
  expect(save).not.toHaveBeenCalled();
  await act(async () => {
    deliver(" saved\n ");
  });
  await waitFor(() => {
    expect(result.current.text).toBe(" saved\n \n new\ntext ");
  });
  expect(result.current.draft.kind).toBe("ready");
});
