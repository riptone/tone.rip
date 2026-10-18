/**
 * What to do with an email once rittea has answered.
 *
 * - `delivered`: 200, rittea stored it and posted it in the ticket's thread.
 * - `ignored`: 202, rittea did not take it (an unknown or forged token, or a
 *   sender who is not the ticket's requester). A human should still see it.
 * - `failed`: anything else. Forwarded too, so nothing is lost, and logged.
 */
export type Outcome = "delivered" | "ignored" | "failed";

const OK = 200;
const ACCEPTED = 202;

export function outcome(status: number): Outcome {
  if (status === OK) return "delivered";
  if (status === ACCEPTED) return "ignored";
  return "failed";
}
