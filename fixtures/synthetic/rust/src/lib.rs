pub fn sum(a: u32, b: u32) -> u32 { a + b }
#[cfg(test)]
mod tests {
    #[test]
    fn adds_whole_numbers() { assert_eq!(super::sum(2, 3), 5); }
}
