export type QuestionSelectionState = Readonly<{
  answer: string;
  askID: string;
  selectedOption: number | null;
  submitted: boolean;
  userSelected: boolean;
}>;

export function emptyQuestionSelection(askID: string): QuestionSelectionState {
  return { answer: "", askID, selectedOption: null, submitted: false, userSelected: false };
}
